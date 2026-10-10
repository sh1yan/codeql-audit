package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/config"
	"codeql-audit/internal/model"
	"codeql-audit/internal/sourcefile"
	"codeql-audit/internal/translate"
	"codeql-audit/internal/verify"
)

// ------------------------- AI 误报验证 -------------------------

// verifier 返回当前的 AI 判定器。
//
// 这里刻意**不复用翻译引擎的客户端**：AI 误报验证有自己的总开关
// （设置 → AI 误报验证 → 启用 AI 误报验证）。以前它借用翻译的
// 「未命中翻译库时调用大模型」开关，用户在设置里填对了接口地址、模型与 Key，
// 只要没勾那个翻译开关，AI 验证就永远报「未配置大模型」。
func (a *App) verifier() *verify.Verifier {
	cfg := toLLMConfig(a.cfg.LLM)
	// 只看 AI 验证自己的开关，不看翻译开关。
	cfg.Enabled = a.cfg.AIVerify()
	client := translate.NewLLMClient(cfg)
	return verify.New(client, cfg.Model)
}

// aiVerifyMissingHint 返回「AI 验证还不能用」时可操作的中文提示。
//
// 把三种缺失情况分开说清楚，用户照着去设置里点一下就能用：
// 没打开总开关、没填接口地址、没填模型名。
func (a *App) aiVerifyMissingHint() string {
	var missing []string
	if !a.cfg.AIVerify() {
		missing = append(missing, "勾选「AI 误报验证 → 启用 AI 误报验证」")
	}
	if strings.TrimSpace(a.cfg.LLM.BaseURL) == "" {
		missing = append(missing, "填写接口地址（OpenAI 兼容，如 https://api.deepseek.com/v1）")
	}
	if strings.TrimSpace(a.cfg.LLM.Model) == "" {
		missing = append(missing, "填写模型名（如 deepseek-chat）")
	}
	if len(missing) == 0 {
		return ""
	}
	return "请在「设置」里：" + strings.Join(missing, "；") + "。"
}

// buildVerifyInput 组装一条漏洞的判定上下文（含源码片段）。
func (a *App) buildVerifyInput(f *model.Finding) verify.Input {
	sendCode := a.cfg.AISendCode()
	in := verify.Input{
		RuleName:    f.RuleName,
		RuleNameZH:  f.RuleNameZH,
		Description: f.Description,
		SeverityZH:  f.Severity.ZH(),
		MessageZH:   f.MessageZHText(),
		MessageEN:   f.MessageENText(),
		SendCode:    sendCode,
	}
	const ctx = 3
	if sendCode {
		if snip, ok := a.snippetFor(f.Sink, ctx); ok {
			in.Sink = &snip
		}
	} else if f.Sink.StartLine > 0 {
		in.Sink = &verify.Snippet{Path: f.Sink.DisplayPath(), Line: f.Sink.StartLine}
	}
	for _, s := range f.Sources {
		if sendCode {
			if snip, ok := a.snippetFor(s, ctx); ok {
				in.Sources = append(in.Sources, snip)
			}
			continue
		}
		if s.StartLine > 0 {
			in.Sources = append(in.Sources, verify.Snippet{Path: s.DisplayPath(), Line: s.StartLine})
		}
	}
	return in
}

// snippetFor 读取一处位置附近的源码片段。
//
// 这里用独立的缓存 + 互斥锁：批量验证在后台 goroutine 里跑，不能复用只属于
// 界面主线程的 fileCache。
func (a *App) snippetFor(loc model.Location, ctx int) (verify.Snippet, bool) {
	path := loc.ResolvedPath
	if path == "" || loc.StartLine <= 0 {
		return verify.Snippet{}, false
	}
	a.snippetMu.Lock()
	lines, ok := a.snippetCache[path]
	a.snippetMu.Unlock()
	if !ok {
		var err error
		lines, _, err = sourcefile.ReadLines(path)
		if err != nil {
			return verify.Snippet{}, false
		}
		a.snippetMu.Lock()
		if a.snippetCache == nil {
			a.snippetCache = map[string][]string{}
		}
		a.snippetCache[path] = lines
		a.snippetMu.Unlock()
	}
	el, _ := loc.NormalizedEnd()
	return verify.BuildSnippet(loc.DisplayPath(), lines, loc.StartLine, el, ctx), true
}

// verifyFinding 对当前选中的漏洞做一次 AI 误报判定。
func (a *App) verifyFinding(f *model.Finding) {
	if f == nil {
		return
	}
	v := a.verifier()
	if !v.Configured() {
		dialog.ShowInformation("未配置大模型",
			"AI 误报验证需要大模型。\n\n"+a.aiVerifyMissingHint()+"\n\n"+
				"隐私说明：可在设置里关闭「AI 验证时发送源码片段」，关闭后只发送位置信息。", a.win)
		return
	}
	if f.HasAIVerdict() {
		a.statsLbl.SetText("正在重新验证，请稍候 …")
	} else {
		a.statsLbl.SetText("正在做 AI 验证，请稍候 …")
	}
	win := a.win
	progress := dialog.NewCustomWithoutButtons("AI 验证中",
		container.NewVBox(widget.NewLabel(fmt.Sprintf("正在验证：%s", f.Sink.Display())),
			widget.NewProgressBarInfinite()), win)
	progress.Resize(fyne.NewSize(420, 150))
	progress.Show()

	input := a.buildVerifyInput(f)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.verifyTimeout())
		defer cancel()
		res, err := v.Verify(ctx, input)
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				a.statsLbl.SetText("AI 验证失败：" + err.Error())
				dialog.ShowError(fmt.Errorf("AI 验证失败：%w", err), win)
				return
			}
			a.applyVerdict(f, res)
			a.statsLbl.SetText(fmt.Sprintf("AI 判定：%s（%s）", f.AIVerdictZH(), f.AIReason))
		})
	}()
}

// verifyTimeout 返回单条 AI 验证的超时时间（跟随大模型配置，至少 30 秒）。
func (a *App) verifyTimeout() time.Duration {
	t := a.cfg.LLM.TimeoutSeconds
	if t <= 0 {
		t = 60
	}
	if t < 30 {
		t = 30
	}
	return time.Duration(t) * time.Second
}

// testLLMConnection 用设置里当前填写的参数真实调用一次大模型，验证配置是否可用。
//
// 存在意义：用户经常「填了内容但验证不了」，却只能看到一句「AI 验证失败」。
// 这里把接口地址、Key、模型名原样试一遍，把 HTTP 状态码、超时、DNS 等
// 真实错误直接摆出来，用户就知道是 Key 错了还是地址写错了。
func (a *App) testLLMConnection(s config.LLMSettings) {
	cfg := toLLMConfig(s)
	// 测试连接只关心「参数能不能通」，所以不受任何开关影响。
	cfg.Enabled = true
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		dialog.ShowInformation("测试连接", "请先填写接口地址与模型名，再测试连接。", a.win)
		return
	}
	client := translate.NewLLMClient(cfg)
	to := s.TimeoutSeconds
	if to < 15 {
		to = 15
	}
	win := a.win
	progress := dialog.NewCustomWithoutButtons("测试连接",
		container.NewVBox(
			widget.NewLabel("正在调用："+cfg.Endpoint()),
			widget.NewLabel("模型："+cfg.Model),
			widget.NewProgressBarInfinite()), win)
	progress.Resize(fyne.NewSize(480, 170))
	progress.Show()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(to)*time.Second)
		defer cancel()
		out, err := client.Chat(ctx, "你是连通性测试助手，只回复 OK。", "请只回复 OK")
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				dialog.ShowError(fmt.Errorf("连接失败：%w", err), win)
				return
			}
			reply := strings.TrimSpace(out)
			if r := []rune(reply); len(r) > 120 {
				reply = string(r[:120]) + "…"
			}
			if reply == "" {
				reply = "（空响应）"
			}
			dialog.ShowInformation("测试连接",
				fmt.Sprintf("连接成功。\n接口：%s\n模型：%s\n回复：%s", cfg.Endpoint(), cfg.Model, reply), win)
		})
	}()
}

// applyVerdict 把一次判定结果写进漏洞对象、列表与配置。
func (a *App) applyVerdict(f *model.Finding, res verify.Result) {
	if f == nil {
		return
	}
	f.AIVerdict = string(res.Verdict)
	f.AIReason = res.Reason
	f.AIConfidence = res.Confidence
	f.AIModel = res.Model
	f.AIAt = time.Now().Format(time.RFC3339)
	key := f.Key()
	a.verdicts[key] = config.VerdictRecord{
		Status: f.AIVerdict, Reason: f.AIReason, Confidence: f.AIConfidence,
		Model: f.AIModel, At: f.AIAt,
	}
	if a.detail.current == f {
		a.detail.Update(f)
	}
	if a.verdictFilter() != "" {
		// AI 判定筛选的结果会随新结论变化，必须重新过滤
		a.refreshList()
	} else if i := a.rowIndex(f); i >= 0 {
		a.list.RefreshItem(widget.ListItemID(i))
	}
	a.updateCountsOnly()
	a.updateStats()
	a.scheduleSave()
}

// clearVerdict 清除一条漏洞的 AI 判定结果。
func (a *App) clearVerdict(f *model.Finding) {
	if f == nil {
		return
	}
	f.AIVerdict, f.AIReason, f.AIConfidence, f.AIModel, f.AIAt = "", "", 0, "", ""
	delete(a.verdicts, f.Key())
	if st := a.projectState(); st != nil {
		st.ClearVerdict(f.Key())
	}
	if a.detail.current == f {
		a.detail.Update(f)
	}
	if i := a.rowIndex(f); i >= 0 {
		a.list.RefreshItem(widget.ListItemID(i))
	}
	a.refreshList()
	a.updateStats()
	a.saveNow()
	a.statsLbl.SetText("已清除该条漏洞的 AI 判定结果")
}
