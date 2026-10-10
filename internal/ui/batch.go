package ui

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/model"
	"codeql-audit/internal/verify"
)

// batchConcurrency 是批量 AI 验证的并发数。
//
// 取 3 是保守值：多数 OpenAI 兼容服务对并发有限制，串行又太慢。
const batchConcurrency = 3

// batchVerifyScope 描述批量验证的范围。
type batchVerifyScope int

const (
	scopeCurrent batchVerifyScope = iota // 当前筛选结果
	scopePending                         // 全部未验证
	scopeAll                             // 全部重新验证
)

// showBatchVerify 打开「批量 AI 验证」对话框。
func (a *App) showBatchVerify() {
	if a.proj == nil {
		dialog.ShowInformation("提示", "请先打开 CodeQL 结果 CSV 文件。", a.win)
		return
	}
	v := a.verifier()
	if !v.Configured() {
		dialog.ShowInformation("未配置大模型",
			"批量 AI 验证需要大模型。\n\n"+a.aiVerifyMissingHint(), a.win)
		return
	}

	scope := scopeCurrent
	scopeSel := widget.NewSelect([]string{}, nil)
	refreshScopeOptions := func() {
		cur := len(a.rows)
		pending := 0
		for _, f := range a.proj.Findings {
			if !f.HasAIVerdict() {
				pending++
			}
		}
		scopeSel.Options = []string{
			fmt.Sprintf("当前筛选结果（%d 条）", cur),
			fmt.Sprintf("全部未验证（%d 条）", pending),
			fmt.Sprintf("全部重新验证（%d 条，会覆盖已有结论）", len(a.proj.Findings)),
		}
		scopeSel.SetSelectedIndex(int(scope))
	}
	scopeSel.OnChanged = func(string) {
		switch scopeSel.SelectedIndex() {
		case 1:
			scope = scopePending
		case 2:
			scope = scopeAll
		default:
			scope = scopeCurrent
		}
	}
	refreshScopeOptions()

	counterLbl := widget.NewLabel("尚未开始")
	progress := widget.NewProgressBar()
	statusLbl := widget.NewLabel("准备就绪。点击「开始验证」后逐条调用大模型，结论会自动保存。")
	statusLbl.Wrapping = fyne.TextWrapWord
	statusLbl.Importance = widget.LowImportance

	var results []*model.Finding
	var resultList *widget.List
	resultList = widget.NewList(
		func() int { return len(results) },
		func() fyne.CanvasObject {
			head := widget.NewLabel("规则占位")
			head.TextStyle = fyne.TextStyle{Bold: true}
			head.Truncation = fyne.TextTruncateEllipsis
			sub := widget.NewLabel("位置与判定占位")
			sub.Truncation = fyne.TextTruncateEllipsis
			reason := widget.NewLabel("理由占位")
			reason.Importance = widget.LowImportance
			reason.Truncation = fyne.TextTruncateEllipsis
			return container.NewVBox(head, sub, reason)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if int(id) >= len(results) {
				return
			}
			f := results[id]
			box := o.(*fyne.Container)
			head := box.Objects[0].(*widget.Label)
			sub := box.Objects[1].(*widget.Label)
			reason := box.Objects[2].(*widget.Label)
			head.SetText(fmt.Sprintf("%s　#%d", f.RuleDisplayName(), f.Index))
			verdict := "未验证"
			if f.HasAIVerdict() {
				verdict = f.AIVerdictZH()
				if f.AIConfidence > 0 {
					verdict += fmt.Sprintf("（置信度 %.0f%%）", f.AIConfidence*100)
				}
			}
			sub.SetText(fmt.Sprintf("%s　·　%s", f.Sink.File, verdict))
			reason.SetText(f.AIReason)
		},
	)
	resultList.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(results) {
			return
		}
		target := results[id]
		resultList.UnselectAll()
		// 跳回主界面：选中这条并定位
		a.refreshList()
		if i := a.rowIndex(target); i >= 0 {
			a.list.Select(i)
			a.openFindingAt(i, true)
		} else {
			a.openFinding(target, true)
		}
	}

	startBtn := widget.NewButtonWithIcon("开始验证", theme.MediaPlayIcon(), nil)
	stopBtn := widget.NewButtonWithIcon("停止", theme.MediaStopIcon(), nil)
	stopBtn.Disable()
	exportBtn := widget.NewButtonWithIcon("导出验证结果 CSV", theme.DocumentSaveIcon(), nil)
	exportBtn.Disable()

	var (
		cancel   context.CancelFunc
		running  bool
		mu       sync.Mutex
		done     int
		total    int
		falsePos int
		confirm  int
		unsure   int
		failed   int
		lastErr  string
	)
	// runBatch 先声明后赋值：startBtn 的回调里会引用它。
	var runBatch func([]*model.Finding)

	updateProgress := func() {
		mu.Lock()
		d, t := done, total
		fp, tp, un, bad, le := falsePos, confirm, unsure, failed, lastErr
		mu.Unlock()
		if t > 0 {
			progress.SetValue(float64(d) / float64(t))
		}
		counterLbl.SetText(fmt.Sprintf("已处理 %d / %d　·　误报 %d　·　确认 %d　·　存疑 %d　·　失败 %d",
			d, t, fp, tp, un, bad))
		msg := fmt.Sprintf("已处理 %d / %d　条。", d, t)
		if le != "" {
			msg += "　最近一次失败：" + le
		}
		statusLbl.SetText(msg)
	}

	targets := func() []*model.Finding {
		switch scope {
		case scopePending:
			out := make([]*model.Finding, 0, len(a.proj.Findings))
			for _, f := range a.proj.Findings {
				if !f.HasAIVerdict() {
					out = append(out, f)
				}
			}
			return out
		case scopeAll:
			return append([]*model.Finding(nil), a.proj.Findings...)
		default:
			return append([]*model.Finding(nil), a.rows...)
		}
	}

	startBtn.OnTapped = func() {
		if running {
			return
		}
		list := targets()
		if len(list) == 0 {
			dialog.ShowInformation("提示", "当前范围内没有需要验证的漏洞。", a.win)
			return
		}
		// 二次确认：批量验证会真实调用大模型（可能产生费用）
		confirmMsg := fmt.Sprintf("将对 %d 条漏洞调用大模型做误报判定。\n范围：%s\n\n"+
			"每条会发送规则名、规则描述、漏洞详情%s。\n继续吗？",
			len(list), scopeSel.Selected, a.aiPrivacyNote())
		dialog.ShowConfirm("确认批量 AI 验证", confirmMsg, func(ok bool) {
			if !ok {
				return
			}
			runBatch(list)
		}, a.win)
	}

	runBatch = func(list []*model.Finding) {
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		running = true
		startBtn.Disable()
		stopBtn.Enable()
		exportBtn.Disable()
		mu.Lock()
		done, total, falsePos, confirm, unsure, failed, lastErr = 0, len(list), 0, 0, 0, 0, ""
		mu.Unlock()
		results = nil
		resultList.Refresh()
		progress.SetValue(0)
		updateProgress()

		// 预先在主线程把上下文（含源码片段）组装好，后台只做大模型调用，
		// 避免后台 goroutine 触碰界面侧的缓存与控件。
		type job struct {
			f     *model.Finding
			input verify.Input
		}
		jobs := make([]job, 0, len(list))
		for _, f := range list {
			jobs = append(jobs, job{f: f, input: a.buildVerifyInput(f)})
		}

		sem := make(chan struct{}, batchConcurrency)
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			go func(j job) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				reqCtx, cancelReq := context.WithTimeout(ctx, a.verifyTimeout())
				res, err := v.Verify(reqCtx, j.input)
				cancelReq()
				fyne.Do(func() {
					results = append(results, j.f)
					if err != nil {
						mu.Lock()
						failed++
						lastErr = err.Error()
						mu.Unlock()
					} else {
						a.applyVerdict(j.f, res)
						mu.Lock()
						switch res.Verdict {
						case verify.FalsePositive:
							falsePos++
						case verify.TruePositive:
							confirm++
						default:
							unsure++
						}
						mu.Unlock()
					}
					mu.Lock()
					done++
					mu.Unlock()
					resultList.Refresh()
					updateProgress()
				})
			}(j)
		}

		go func() {
			wg.Wait()
			fyne.Do(func() {
				running = false
				startBtn.Enable()
				stopBtn.Disable()
				exportBtn.Enable()
				cancel = nil
				a.saveNow()
				mu.Lock()
				d, fp, tp, un, bad := done, falsePos, confirm, unsure, failed
				mu.Unlock()
				statusLbl.SetText(fmt.Sprintf(
					"完成：共 %d 条，误报 %d，确认 %d，存疑 %d，失败 %d。结论已保存，可点列表回到对应漏洞复核。",
					d, fp, tp, un, bad))
				counterLbl.SetText(fmt.Sprintf("已处理 %d / %d", d, total))
			})
		}()
	}

	stopBtn.OnTapped = func() {
		if cancel != nil {
			cancel()
			statusLbl.SetText("正在停止 …（已在途的请求会先返回）")
		}
	}

	exportBtn.OnTapped = func() {
		a.exportVerdicts()
	}

	head := container.NewVBox(
		widget.NewLabelWithStyle("批量 AI 误报验证", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, widget.NewLabel("验证范围"), nil, scopeSel),
		container.NewHBox(startBtn, stopBtn, exportBtn, layout.NewSpacer()),
		counterLbl,
		progress,
		statusLbl,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("结果（点击一条可跳回主界面复核）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	content := container.NewBorder(head, nil, nil, nil, resultList)
	d := dialog.NewCustom("批量 AI 验证", "关闭", content, a.win)
	d.SetOnClosed(func() {
		if cancel != nil {
			cancel()
		}
	})
	d.Resize(fyne.NewSize(920, 680))
	d.Show()
}

// aiPrivacyNote 描述当前 AI 验证会把什么发给大模型。
func (a *App) aiPrivacyNote() string {
	if a.cfg.AISendCode() {
		return "以及沉淀点/来源点附近的源码片段"
	}
	return "（隐私模式：不发送源码片段）"
}

// exportVerdicts 把当前项目的 AI 判定结论导出成 CSV。
func (a *App) exportVerdicts() {
	if a.proj == nil {
		return
	}
	d := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil {
			a.showError(err)
			return
		}
		if wc == nil {
			return
		}
		path := wc.URI().Path()
		_ = wc.Close()
		if err := writeVerdictCSV(path, a.proj.Findings); err != nil {
			a.showError(err)
			return
		}
		count := 0
		for _, f := range a.proj.Findings {
			if f.HasAIVerdict() {
				count++
			}
		}
		dialog.ShowInformation("导出成功",
			fmt.Sprintf("已导出 %d 条 AI 判定结论到：\n%s", count, path), a.win)
	}, a.win)
	d.SetFileName("AI验证结果.csv")
	d.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
	showFileDialog(d, fyne.NewSize(940, 620))
}

// oneLine 把多行理由压成一行，方便在 Excel 里阅读与筛选。
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

// writeVerdictCSV 写出一份带 UTF-8 BOM 的验证结果表，Excel 双击不乱码。
func writeVerdictCSV(path string, findings []*model.Finding) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"序号", "规则名称", "严重级别", "文件", "起始行", "起始列",
		"AI 判定", "置信度", "判定理由", "模型", "判定时间"})
	for _, fd := range findings {
		if !fd.HasAIVerdict() {
			continue
		}
		conf := ""
		if fd.AIConfidence > 0 {
			conf = fmt.Sprintf("%.0f%%", fd.AIConfidence*100)
		}
		_ = w.Write([]string{
			fmt.Sprintf("%d", fd.Index), fd.RuleDisplayName(), fd.Severity.ZH(),
			fd.Sink.File, fmt.Sprintf("%d", fd.Sink.StartLine), fmt.Sprintf("%d", fd.Sink.StartCol),
			fd.AIVerdictZH(), conf, oneLine(fd.AIReason),
			fd.AIModel, fd.AIAt,
		})
	}
	w.Flush()
	return w.Error()
}
