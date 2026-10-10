package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/model"
)

func severityColor(s model.Severity) color.Color {
	switch s {
	case model.SevError:
		return color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF}
	case model.SevWarning:
		return color.NRGBA{R: 0xEA, G: 0x58, B: 0x0C, A: 0xFF}
	case model.SevNote:
		return color.NRGBA{R: 0x0E, G: 0x74, B: 0x90, A: 0xFF}
	}
	return color.NRGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xFF}
}

// wrapLabel 创建自动换行的标签。
//
// 必须注意：Fyne 中 Wrapping 为 TextWrapOff 的 Label，其 MinSize 等于整段文本
// 的宽度。详情面板里只要有一句长英文（规则名、描述、英文原文）没有换行，
// 右侧面板的最小宽度就会被撑到上千像素，三栏布局随即被挤死，
// 分隔条也就再也拖不动了（HSplit 在 min > max 时会完全忽略 offset）。
// 详情面板中的长文本一律用本函数创建。
func wrapLabel(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// selectableWrapLabel 与 wrapLabel 相同，但允许用鼠标选中并复制
// （拖拽选中后 Cmd/Ctrl+C，或右键菜单里的 Copy）。
//
// 详情面板里的说明文字全部用它。证据链的每一行不能用：chainRow 是「整行可点的
// 跳转按钮」，而 Label 一旦可选中就会挂一层 selectable 覆盖层，它自己实现了
// Tapped，会把点击吞掉，「点证据链跳转」随即失效（见 chainrow.go）。
func selectableWrapLabel(text string) *widget.Label {
	l := wrapLabel(text)
	l.Selectable = true
	return l
}

// severityBadge 返回一个带底色的严重级别标签。
func severityBadge(s model.Severity) fyne.CanvasObject {
	bg := canvas.NewRectangle(severityColor(s))
	bg.CornerRadius = 9
	lbl := widget.NewLabel(s.ZH())
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	lbl.Alignment = fyne.TextAlignCenter
	return container.NewStack(bg, container.NewCenter(lbl))
}

// verdictColor 返回 AI 判定徽章的颜色。
func verdictColor(v string) color.Color {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false_positive":
		return color.NRGBA{R: 0x15, G: 0x80, B: 0x3D, A: 0xFF} // 绿：误报
	case "true_positive":
		return color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF} // 红：真实漏洞
	case "uncertain":
		return color.NRGBA{R: 0xD9, G: 0x77, B: 0x06, A: 0xFF} // 橙：存疑
	}
	return color.NRGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xFF}
}

// findingRow 是漏洞列表中的一行。
//
// 这里刻意使用「自定义 Widget + 显式渲染器」而不是嵌入 fyne.Container：
// Fyne 的 List 只在首次拿到模板项时测量一次尺寸，之后才填充内容，
// 依赖容器自动布局会拿不到正确的行高与宽度（实测行内容完全不显示）。
type findingRow struct {
	widget.BaseWidget
	dotColor color.Color
	ruleText string
	fileText string

	// 已读状态：未读的行左侧有一条蓝色竖条，规则名加粗；已读的行整行变灰。
	read bool
	// AI 判定：非空时右侧显示「AI 误报 / AI 确认 / AI 存疑」徽章。
	verdict     string
	verdictText string

	// id 是这一行在列表中的下标；onTapped 由列表在填充行内容时注入。
	// 自己实现 Tapped 的原因：Fyne 的 List.Select 对「已选中项」会直接返回，
	// 不会回调 OnSelected，于是重复点击同一行时无法重新定位。
	id       int
	onTapped func(id int)
}

func newFindingRow() *findingRow {
	r := &findingRow{}
	r.ExtendBaseWidget(r)
	return r
}

// Tapped 处理行点击（含点击行内文字），交给 App 统一定位。
func (r *findingRow) Tapped(*fyne.PointEvent) {
	if r.onTapped != nil {
		r.onTapped(r.id)
	}
}

// rowHeight 是列表行高。
const rowHeight = 48

// verdictBadgeWidth 是右侧 AI 徽章的固定宽度。
const verdictBadgeWidth = 60

func (r *findingRow) Set(f *model.Finding) {
	r.dotColor = severityColor(f.Severity)
	name := f.RuleDisplayName()
	if f.TranslatePending {
		name = "⚠ " + name
	}
	r.ruleText = name
	loc := f.Sink.File
	if f.Sink.StartLine > 0 {
		loc = fmt.Sprintf("%s:%d", f.Sink.File, f.Sink.StartLine)
	}
	if !f.Sink.Resolved() {
		loc += "  [未定位]"
	}
	r.fileText = loc
	r.verdict = f.AIVerdict
	r.verdictText = model.AIVerdictShort(f.AIVerdict)
	r.Refresh()
}

// SetRead 更新已读状态（不改变其它内容）。
func (r *findingRow) SetRead(read bool) {
	if r.read == read {
		return
	}
	r.read = read
	r.Refresh()
}

// MinSize 返回固定行高，必须在填充内容前后保持一致。
func (r *findingRow) MinSize() fyne.Size { return fyne.NewSize(240, rowHeight) }

func (r *findingRow) CreateRenderer() fyne.WidgetRenderer {
	rr := &findingRowRenderer{row: r}
	rr.unread = canvas.NewRectangle(color.NRGBA{R: 0x1D, G: 0x4E, B: 0xD8, A: 0xFF})
	rr.dot = canvas.NewCircle(color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF})
	rr.rule = widget.NewLabel("使用用户可控数据拼接 SQL 查询")
	rr.rule.TextStyle = fyne.TextStyle{Bold: true}
	rr.rule.Truncation = fyne.TextTruncateEllipsis
	rr.file = widget.NewLabel("app/common/sql.py:16")
	rr.file.Importance = widget.LowImportance
	rr.file.Truncation = fyne.TextTruncateEllipsis
	rr.badgeBG = canvas.NewRectangle(color.Transparent)
	rr.badgeBG.CornerRadius = 8
	rr.badge = canvas.NewText("", color.White)
	rr.badge.Alignment = fyne.TextAlignCenter
	rr.badge.TextStyle = fyne.TextStyle{Bold: true}
	rr.objects = []fyne.CanvasObject{rr.unread, rr.dot, rr.badgeBG, rr.rule, rr.file, rr.badge}
	return rr
}

type findingRowRenderer struct {
	row     *findingRow
	unread  *canvas.Rectangle
	dot     *canvas.Circle
	rule    *widget.Label
	file    *widget.Label
	badgeBG *canvas.Rectangle
	badge   *canvas.Text
	objects []fyne.CanvasObject
}

func (rr *findingRowRenderer) Layout(size fyne.Size) {
	const gutter = 24
	h := size.Height
	if h < 10 {
		h = rowHeight
	}

	// 未读竖条
	rr.unread.Move(fyne.NewPos(0, 2))
	rr.unread.Resize(fyne.NewSize(3, h-4))
	if rr.row.read {
		rr.unread.Hide()
	} else {
		rr.unread.Show()
	}

	rr.dot.FillColor = rr.row.dotColor
	rr.dot.Move(fyne.NewPos(9, h/2-5))
	rr.dot.Resize(fyne.NewSize(10, 10))

	badgeW := float32(0)
	if rr.row.verdictText != "" {
		badgeW = verdictBadgeWidth
		rr.badgeBG.FillColor = verdictColor(rr.row.verdict)
		rr.badgeBG.Move(fyne.NewPos(size.Width-badgeW-6, h/2-10))
		rr.badgeBG.Resize(fyne.NewSize(badgeW, 20))
		rr.badgeBG.Show()
		rr.badge.Text = rr.row.verdictText
		rr.badge.TextSize = theme.Current().Size(theme.SizeNameCaptionText)
		rr.badge.Move(fyne.NewPos(size.Width-badgeW-6, h/2-10))
		rr.badge.Resize(fyne.NewSize(badgeW, 20))
		rr.badge.Show()
	} else {
		rr.badgeBG.Hide()
		rr.badge.Hide()
	}

	text := rr.row.ruleText
	if rr.row.read {
		// 已读：整体降级显示，避免在列表里抢注意力
		rr.rule.TextStyle = fyne.TextStyle{}
		rr.rule.Importance = widget.LowImportance
	} else {
		rr.rule.TextStyle = fyne.TextStyle{Bold: true}
		rr.rule.Importance = widget.MediumImportance
	}
	rr.rule.SetText(text)
	rr.rule.Move(fyne.NewPos(gutter, 0))
	rr.rule.Resize(fyne.NewSize(size.Width-gutter-badgeW-8, h*0.52))

	// 已读标记放在第二行（文件:行号）末尾：中文环境下 "✓" 在很多中文字体里
	// 缺字形会渲染成怪符号，用汉字更稳，也不占用规则名的宽度。
	fileText := rr.row.fileText
	if rr.row.read {
		fileText += "　·　已读"
	}
	rr.file.SetText(fileText)
	rr.file.Move(fyne.NewPos(gutter, h*0.5))
	rr.file.Resize(fyne.NewSize(size.Width-gutter-badgeW-8, h*0.48))
}

func (rr *findingRowRenderer) MinSize() fyne.Size { return fyne.NewSize(240, rowHeight) }

func (rr *findingRowRenderer) Refresh() {
	rr.Layout(rr.row.Size())
	canvas.Refresh(rr.row)
}

func (rr *findingRowRenderer) Objects() []fyne.CanvasObject { return rr.objects }

func (rr *findingRowRenderer) Destroy() {}

// ------------------------- 详情面板 -------------------------

// DetailPane 是右侧详情面板。
type DetailPane struct {
	root    *fyne.Container
	scroll  *container.Scroll
	ruleZH  *widget.Label
	ruleEN  *widget.Label
	sevBox  *fyne.Container
	metaLbl *widget.Label
	descZH  *widget.Label
	descEN  *widget.Label
	msgBox  *fyne.Container
	engBox  *fyne.Container
	chain   *fyne.Container
	actions *fyne.Container

	aiBox    *fyne.Container
	aiBadge  *fyne.Container
	aiMeta   *widget.Label
	aiStatus *widget.Label
	aiReason *widget.Label
	aiBtns   *fyne.Container

	current *model.Finding
	// active 记录当前聚焦的证据链条目（"sink" / "src:N"），用于高亮它。
	active string

	onLink         func(*model.LinkRef)
	onJump         func(model.Location, HLKind)
	onCorrect      func(f *model.Finding)
	onCopy         func()
	onVerify       func(f *model.Finding)
	onClearVerdict func(f *model.Finding)
	lang           string
}

// NewDetailPane 创建详情面板。
func NewDetailPane() *DetailPane {
	d := &DetailPane{lang: "zh"}
	d.ruleZH = selectableWrapLabel("")
	d.ruleZH.TextStyle = fyne.TextStyle{Bold: true}
	d.ruleEN = selectableWrapLabel("")
	d.ruleEN.Importance = widget.LowImportance
	d.sevBox = container.NewHBox()
	d.metaLbl = selectableWrapLabel("")
	d.metaLbl.Importance = widget.LowImportance
	d.descZH = selectableWrapLabel("")
	d.descEN = selectableWrapLabel("")
	d.descEN.Importance = widget.LowImportance
	d.msgBox = container.NewVBox()
	d.engBox = container.NewVBox()
	d.chain = container.NewVBox()
	d.actions = container.NewHBox()

	d.aiStatus = selectableWrapLabel("尚未验证")
	d.aiMeta = selectableWrapLabel("")
	d.aiMeta.Importance = widget.LowImportance
	d.aiReason = selectableWrapLabel("")
	d.aiReason.Importance = widget.LowImportance
	d.aiBadge = container.NewHBox()
	d.aiBtns = container.NewHBox()
	d.aiBox = container.NewVBox(d.aiBadge, d.aiMeta, d.aiStatus, d.aiReason, d.aiBtns)

	title := widget.NewLabelWithStyle("漏洞详情", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.root = container.NewVBox(
		title,
		d.ruleZH, d.ruleEN,
		container.NewHBox(d.sevBox),
		d.metaLbl,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("AI 误报验证", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.aiBox,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("规则描述", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.descZH, d.descEN,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("漏洞详情（中文）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.msgBox,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("证据链", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.chain,
		widget.NewSeparator(),
		d.actions,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("英文原文", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		d.engBox,
	)
	// 内容外面再包一层 clampWidth：右栏的最小宽度必须由我们说了算，
	// 否则证据链按钮（Button 不支持换行）会把右栏撑到 600px 以上，分隔条就拖不动了。
	d.scroll = container.NewVScroll(clampWidth(container.NewPadded(d.root), detailMinWidth))
	return d
}

// Object 返回可放置组件。
func (d *DetailPane) Object() fyne.CanvasObject { return d.scroll }

// SetLang 设置显示语言：zh / en / both。
func (d *DetailPane) SetLang(lang string) {
	d.lang = lang
	if d.current != nil {
		d.Update(d.current)
	}
}

// SetActive 标记当前已经跳转过去的证据链条目（"sink" 或 "src:N"，空串表示无）。
func (d *DetailPane) SetActive(key string) {
	if d.active == key {
		return
	}
	d.active = key
	if d.current != nil {
		d.rebuildChain(d.current)
	}
}

// Update 刷新某条漏洞的详情。
func (d *DetailPane) Update(f *model.Finding) {
	d.current = f
	if f == nil {
		d.ruleZH.SetText("请在左侧选择一条漏洞")
		d.ruleEN.SetText("")
		d.sevBox.RemoveAll()
		d.metaLbl.SetText("")
		d.descZH.SetText("")
		d.descEN.SetText("")
		d.msgBox.RemoveAll()
		d.chain.RemoveAll()
		d.actions.RemoveAll()
		d.engBox.RemoveAll()
		d.aiStatus.SetText("尚未验证")
		d.aiStatus.Show()
		d.aiMeta.SetText("")
		d.aiReason.SetText("")
		d.aiBadge.RemoveAll()
		d.aiBtns.RemoveAll()
		return
	}

	d.ruleZH.SetText(fmt.Sprintf("%s　#%d", f.RuleDisplayName(), f.Index))
	d.ruleEN.SetText(f.RuleName)
	d.sevBox.RemoveAll()
	// 严重级别的徽章已经写着「严重」了，旁边再写一句「严重级别：严重（error）」
	// 就是把同一句话说两遍（用户反馈的重复显示）。这里只保留徽章，
	// 文字部分退回去只补 CodeQL 的原始值，避免丢掉「error」这个可检索的信息。
	d.sevBox.Add(severityBadge(f.Severity))
	if raw := strings.TrimSpace(string(f.Severity)); raw != "" && raw != f.Severity.ZH() {
		d.sevBox.Add(widget.NewLabel("CodeQL 原始级别：" + raw))
	}

	src := "内置词典/翻译库"
	switch f.TranslateSource {
	case "llm":
		src = "大模型翻译"
	case "none":
		src = "未翻译（保持英文）"
	}
	if f.TranslatePending {
		src = "待翻译：请在设置中配置大模型，或手工纠错"
	}
	d.metaLbl.SetText(fmt.Sprintf("%s　·　译文来源：%s", d.sinkSummary(f.Sink), src))

	showZH := d.lang == "zh" || d.lang == "both"
	showEN := d.lang == "en" || d.lang == "both"
	if showZH {
		d.ruleZH.Show()
		d.descZH.Show()
	} else {
		d.ruleZH.Hide()
		d.descZH.Hide()
	}
	if showEN {
		d.ruleEN.Show()
		d.descEN.Show()
	} else {
		d.ruleEN.Hide()
		d.descEN.Hide()
	}
	d.descZH.SetText(f.DescriptionDisplay())
	d.descEN.SetText(f.Description)

	// 中文消息：链接可点击
	d.msgBox.RemoveAll()
	if showZH && len(f.MessageZH) > 0 {
		d.msgBox.Add(d.richText(f.MessageZH, false))
	}
	if !showZH || len(f.MessageZH) == 0 {
		d.msgBox.Add(d.richText(f.MessageEN, true))
	}

	d.rebuildAI(f)
	d.rebuildChain(f)

	// 操作按钮
	d.actions.RemoveAll()
	if f.TranslatePending {
		d.actions.Add(widget.NewLabel("⚠ 本条未翻译"))
	}
	d.actions.Add(widget.NewButton("译得不对？纠错", func() {
		if d.onCorrect != nil {
			d.onCorrect(f)
		}
	}))
	d.actions.Add(widget.NewButtonWithIcon("复制中文", theme.ContentCopyIcon(), func() {
		if d.onCopy != nil {
			d.onCopy()
		}
	}))

	// 英文原文
	d.engBox.RemoveAll()
	eng := selectableWrapLabel(f.MessageRaw)
	eng.TextStyle = fyne.TextStyle{Monospace: true}
	d.engBox.Add(eng)
	d.engBox.Add(selectableWrapLabel("规则名：" + f.RuleName))
	d.engBox.Add(selectableWrapLabel("描述：" + f.Description))
}

// sinkSummary 把沉淀点写清楚：文件 · 第 N 行 · 列范围。
//
// 之所以不用 "file:line:col" 这种紧凑写法，是因为用户会把列号误读成行号；
// 证据链里的行号必须和代码视图左侧的行号一一对应，所以这里显式标注单位。
func (d *DetailPane) sinkSummary(loc model.Location) string {
	parts := []string{loc.DisplayPath()}
	parts = append(parts, "第 "+loc.LineRange()+" 行")
	if c := loc.ColumnRange(); c != "" {
		parts = append(parts, "列 "+c)
	}
	if !loc.Resolved() {
		parts = append(parts, "未定位")
	}
	return strings.Join(parts, "　·　")
}

// chainLabel 生成证据链条目的按钮文字。
func (d *DetailPane) chainLabel(tag string, loc model.Location) string {
	label := tag
	if label != "" {
		label += " "
	}
	label += fmt.Sprintf("%s　第 %s 行", loc.DisplayPath(), loc.LineRange())
	if c := loc.ColumnRange(); c != "" {
		label += "　列 " + c
	}
	if t := loc.DisplayLabel(); t != "" {
		label += "　·　" + t
	}
	if !loc.Resolved() {
		label += "　[未定位]"
	}
	return label + "　→ 跳转"
}

// rebuildChain 重建证据链。
//
// 每个条目都带上明确的「第 N 行」，并在跳转后把自己标成 ▶，方便用户确认
// 「当前代码视图里高亮的到底是链上的哪一项」。
//
// 条目用 chainRow（自绘的可换行整行）而不是 widget.Button：Fyne 的 Button
// 不支持换行，右栏一窄就会把「第 N 行 · 标签 · → 跳转」直接裁掉。
func (d *DetailPane) rebuildChain(f *model.Finding) {
	if f == nil {
		return
	}
	d.chain.RemoveAll()
	mark := func(key string) string {
		if d.active == key {
			return "▶ "
		}
		return ""
	}
	d.chain.Add(newChainRow(mark("sink")+"【沉淀点】"+d.chainLabel("", f.Sink),
		theme.VisibilityIcon(), func() {
			d.SetActive("sink")
			if d.onJump != nil {
				d.onJump(f.Sink, HLSink)
			}
		}))
	for i, s := range f.Sources {
		loc := s
		key := fmt.Sprintf("src:%d", i+1)
		d.chain.Add(newChainRow(mark(key)+fmt.Sprintf("【来源 %d】", i+1)+d.chainLabel("", loc),
			theme.VisibilityIcon(), func() {
				d.SetActive(key)
				if d.onJump != nil {
					d.onJump(loc, HLSource)
				}
			}))
	}
	hint := selectableWrapLabel("说明：行号/列号直接取自 CodeQL CSV 的「起始行/起始列」列，以及漏洞详情里内嵌的 " +
		"relative:/// 位置链接（格式为 文件:起始行:起始列:结束行:结束列），未做任何加减。若与本地源码对不上，" +
		"通常是本地源码版本与生成数据库时不一致；跳转后代码视图顶部会提示行号是否越界。")
	hint.Importance = widget.LowImportance
	d.chain.Add(hint)
}

// rebuildAI 重建 AI 误报验证区块。
func (d *DetailPane) rebuildAI(f *model.Finding) {
	d.aiBtns.RemoveAll()
	d.aiBadge.RemoveAll()
	if f == nil {
		d.aiMeta.SetText("")
		d.aiReason.SetText("")
		return
	}
	if !f.HasAIVerdict() {
		d.aiMeta.SetText("")
		d.aiStatus.SetText("尚未验证")
		d.aiStatus.Importance = widget.LowImportance
		d.aiStatus.Show()
		d.aiReason.SetText("让大模型结合规则语义与沉淀点/来源点代码片段，判断这条告警是否为误报。" +
			"判定只作参考，最终结论仍需人工确认。")
		d.aiReason.Show()
		d.aiBtns.Add(widget.NewButtonWithIcon("AI 验证本条", theme.ConfirmIcon(), func() {
			if d.onVerify != nil {
				d.onVerify(f)
			}
		}))
		return
	}
	d.aiStatus.Hide()
	// 徽章单独一行，说明文字用可换行的 Label，避免长文本把面板撑宽。
	// 徽章已经写着「误报 / 真实漏洞 / 存疑」，说明行里就不再重复结论本身，
	// 只留置信度、模型与时间（以前会写成「AI 判定：误报　·　置信度 …」，
	// 和徽章重复，用户反馈过）。
	d.aiBadge.Add(severityBadgeForVerdict(f.AIVerdict))
	meta := make([]string, 0, 3)
	if f.AIConfidence > 0 {
		meta = append(meta, fmt.Sprintf("置信度 %.0f%%", f.AIConfidence*100))
	}
	if f.AIModel != "" {
		meta = append(meta, "模型 "+f.AIModel)
	}
	if len(f.AIAt) >= 16 {
		meta = append(meta, strings.ReplaceAll(f.AIAt[:16], "T", " "))
	}
	d.aiMeta.SetText(strings.Join(meta, "　·　"))
	if strings.TrimSpace(f.AIReason) != "" {
		d.aiReason.SetText("理由：" + f.AIReason)
		d.aiReason.Show()
	} else {
		d.aiReason.Hide()
	}
	d.aiBtns.Add(widget.NewButtonWithIcon("重新验证", theme.ViewRefreshIcon(), func() {
		if d.onVerify != nil {
			d.onVerify(f)
		}
	}))
	d.aiBtns.Add(widget.NewButton("清除结果", func() {
		if d.onClearVerdict != nil {
			d.onClearVerdict(f)
		}
	}))
}

// severityBadgeForVerdict 生成 AI 判定结论的彩色徽章。
func severityBadgeForVerdict(v string) fyne.CanvasObject {
	bg := canvas.NewRectangle(verdictColor(v))
	bg.CornerRadius = 9
	lbl := canvas.NewText(model.AIVerdictZH(v), color.White)
	lbl.Alignment = fyne.TextAlignCenter
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.NewCenter(lbl))
}

func (d *DetailPane) richText(segs []model.Segment, plain bool) fyne.CanvasObject {
	items := make([]widget.RichTextSegment, 0, len(segs)+1)
	for _, s := range segs {
		if s.Kind == model.SegLink && s.Link != nil {
			if plain {
				items = append(items, &widget.TextSegment{
					Text:  "[" + s.Text + " @ " + s.Link.Display() + "]",
					Style: widget.RichTextStyle{Inline: true},
				})
				continue
			}
			link := s.Link
			text := s.Text
			if strings.TrimSpace(text) == "" {
				text = link.Label
			}
			items = append(items, &widget.HyperlinkSegment{
				Text: text,
				OnTapped: func() {
					if d.onLink != nil {
						d.onLink(link)
					}
				},
			})
			continue
		}
		txt := s.Text
		if txt == "" {
			continue
		}
		items = append(items, &widget.TextSegment{
			Text:  txt,
			Style: widget.RichTextStyle{Inline: true},
		})
	}
	rt := widget.NewRichText(items...)
	rt.Wrapping = fyne.TextWrapWord
	return newCopyableText(rt)
}

// copyableText 给 RichText 补一个右键「复制本段文字」入口。
//
// 为什么不直接换成可选中复制的 Label：漏洞详情正文里混着可点击的 relative:///
// 位置链接，必须用 RichText 渲染（Fyne 的 RichText 目前不支持文字选中，只有
// Label 支持）。于是外面包一层专接右键，让用户至少能用鼠标把这段文字取走；
// 面板里其余说明文字都是 selectableWrapLabel，可以正常拖拽选中。
type copyableText struct {
	widget.BaseWidget
	rt *widget.RichText
}

func newCopyableText(rt *widget.RichText) *copyableText {
	c := &copyableText{rt: rt}
	c.ExtendBaseWidget(c)
	return c
}

func (c *copyableText) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.rt)
}

// TappedSecondary 在右键处弹出「复制本段文字」菜单。
func (c *copyableText) TappedSecondary(ev *fyne.PointEvent) {
	app := fyne.CurrentApp()
	if app == nil {
		return
	}
	canvas := app.Driver().CanvasForObject(c)
	if canvas == nil {
		return
	}
	text := c.rt.String()
	menu := fyne.NewMenu("", fyne.NewMenuItem("复制本段文字", func() {
		app.Clipboard().SetContent(text)
	}))
	widget.ShowPopUpMenuAtPosition(menu, canvas, ev.AbsolutePosition)
}
