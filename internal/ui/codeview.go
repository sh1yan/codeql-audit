package ui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// HLKind 表示高亮类型。
type HLKind int

const (
	HLNone   HLKind = iota
	HLSink          // 漏洞沉淀点（红）
	HLSource        // 数据流来源点（黄）
	// HLSearch / HLSearchActive 是「在当前代码文件中查找」的命中高亮。
	//
	// 它们只进 cv.searchSpans（专门的命中层），不进 cv.hl/cv.spans：
	// 行背景色留给沉淀点/来源点，查找命中只画一层半透明色块叠在上面，
	// 这样「一行既是沉淀点又命中查找词」时两种信息都还看得见。
	HLSearch       // 查找命中（浅蓝）
	HLSearchActive // 当前查找命中（琥珀）
)

// Span 是行内精确列范围（1-based，左闭右开）。EndCol 超过行长时表示到行尾。
type Span struct {
	StartCol int
	EndCol   int
	Kind     HLKind

	// OrigStart / OrigEnd 是 CSV 里给出的原始列号（单点位置时两者相等）。
	// 列标尺用它显示「第几列」，从而与右栏证据链的列号完全一致——
	// StartCol/EndCol 在渲染前会被补齐/裁剪，不适合直接展示给用户。
	OrigStart int
	OrigEnd   int
}

// spanToLineEnd 是「一段高亮一直延续到行尾」的哨兵值（见 AddRange）。
const spanToLineEnd = 1 << 30

// 配色
var (
	colorSinkRow    = color.NRGBA{R: 0xFF, G: 0xE8, B: 0xE8, A: 0xFF}
	colorSinkSpan   = color.NRGBA{R: 0xFF, G: 0xB0, B: 0xB0, A: 0xFF}
	colorSourceRow  = color.NRGBA{R: 0xFF, G: 0xF8, B: 0xE1, A: 0xFF}
	colorSourceSpan = color.NRGBA{R: 0xFF, G: 0xDE, B: 0x8C, A: 0xFF}
	colorGutterText = color.NRGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0xFF}
	colorCodeText   = color.NRGBA{R: 0x1E, G: 0x29, B: 0x3B, A: 0xFF}
	// 查找高亮用半透明色：即使叠在沉淀点（红）/来源点（黄）上，底下的漏洞信息也不会被盖死。
	colorSearchSpan   = color.NRGBA{R: 0x93, G: 0xC5, B: 0xFD, A: 0xCC}
	colorSearchActive = color.NRGBA{R: 0xFB, G: 0xBF, B: 0x24, A: 0xE6}
	// colorSelectSpan 是鼠标选区色块（见 codeselect.go）。
	// 半透明：叠在沉淀点/来源点/查找命中之上时，底下的漏洞信息仍然看得见。
	colorSelectSpan = color.NRGBA{R: 0x2E, G: 0x6C, B: 0xE6, A: 0x80}
	// ColorLink 是界面中链接的颜色。
	ColorLink = color.NRGBA{R: 0x1D, G: 0x4E, B: 0xD8, A: 0xFF}
)

const (
	gutterWidth     = 56
	maxSpansPerLine = 6
	// spanFocusMargin 是窗口化时高亮范围左侧保留的字符数
	spanFocusMargin = 40
	// maxDisplayRunes 是单行最多渲染的字符数，避免超长行拖慢测量
	maxDisplayRunes = 4000
)

func codeTextSize() float32 { return theme.Current().Size(theme.SizeNameText) }

func codeStyle() fyne.TextStyle { return fyne.TextStyle{Monospace: true} }

func lineHeight() float32 {
	return fyne.MeasureText("Ag", codeTextSize(), codeStyle()).Height + 6
}

func rowColor(k HLKind) color.Color {
	switch k {
	case HLSink:
		return colorSinkRow
	case HLSource:
		return colorSourceRow
	}
	return color.Transparent
}

func spanColor(k HLKind) color.Color {
	switch k {
	case HLSink:
		return colorSinkSpan
	case HLSource:
		return colorSourceSpan
	case HLSearch:
		return colorSearchSpan
	case HLSearchActive:
		return colorSearchActive
	}
	return color.Transparent
}

func mergeKind(a, b HLKind) HLKind {
	if a == HLSink || b == HLSink {
		return HLSink
	}
	if a == HLSource || b == HLSource {
		return HLSource
	}
	return HLNone
}

// sliceRunes 安全地按 rune 切片（1-based 列号，越界自动裁剪）。
func sliceRunes(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	rs := []rune(s)
	if start >= len(rs) {
		return ""
	}
	if end > len(rs) {
		end = len(rs)
	}
	return string(rs[start:end])
}

// windowTextShift 在必要时横向裁剪文本，让高亮列范围落在可视区域内，
// 并返回被裁掉的 rune 数（0 表示没裁）。
//
// 这样即使漏洞位于超长行的很右侧，用户点开后也能直接看到；偏移量还要给三处复用：
// 列标尺把窗口列号还原成原始列号（见 ruler.go），以及查找高亮跟着一起平移（见 shiftSpans）。
func windowTextShift(text string, spans []Span) (string, []Span, int) {
	rs := []rune(text)
	if len(rs) > maxDisplayRunes {
		rs = rs[:maxDisplayRunes]
		text = string(rs)
	}
	minStart := -1
	for _, sp := range spans {
		if sp.StartCol > 0 && (minStart < 0 || sp.StartCol < minStart) {
			minStart = sp.StartCol
		}
	}
	if minStart <= spanFocusMargin+1 {
		return text, spans, 0
	}
	shift := minStart - 1 - spanFocusMargin
	if shift >= len(rs) {
		shift = len(rs) - 1
	}
	if shift <= 0 {
		return text, spans, 0
	}
	out := "…" + string(rs[shift:])
	// "…" 占 1 个 rune，因此在窗口化文本中的列 = 原列 - shift + 1
	adjusted := make([]Span, 0, len(spans))
	for _, sp := range spans {
		start := sp.StartCol - shift + 1
		end := sp.EndCol - shift + 1
		if start < 2 {
			start = 2 // 不要把 "…" 本身算进高亮
		}
		if end <= start {
			end = start + 1
		}
		adjusted = append(adjusted, Span{
			StartCol: start, EndCol: end, Kind: sp.Kind,
			OrigStart: sp.OrigStart, OrigEnd: sp.OrigEnd,
		})
	}
	return out, adjusted, shift
}

// codeLine 是代码视图中的一行（行背景 + 精确列高亮 + 查找命中高亮 + 鼠标选区）。
type codeLine struct {
	widget.BaseWidget
	lineNo int
	kind   HLKind
	// view 指回所属代码视图：选区状态、列宽换算、右键菜单都在那边（见 codeselect.go）。
	view *CodeView

	// raw* 是「原始 rune 列」的内容，text/spans/search/sel 是横向裁剪后真正渲染的内容。
	// 漏洞高亮、查找命中、鼠标选区必须共用同一次裁剪（windowTextShift 的偏移量），
	// 否则长行被裁剪后几种高亮的列位置会错开。
	rawText   string
	rawSpans  []Span
	rawSearch []Span
	rawSel    [2]int
	hasSel    bool

	text   string
	spans  []Span
	search []Span
	sel    []Span
}

func newCodeLine(view *CodeView) *codeLine {
	c := &codeLine{view: view}
	c.ExtendBaseWidget(c)
	return c
}

// Set 更新行内容（行号、文本、漏洞高亮）。
func (c *codeLine) Set(lineNo int, text string, kind HLKind, spans []Span) {
	c.lineNo, c.rawText, c.kind, c.rawSpans = lineNo, text, kind, spans
	c.apply()
}

// SetSearch 更新这一行的查找命中高亮（列号为原始 rune 列）。
func (c *codeLine) SetSearch(spans []Span) {
	c.rawSearch = spans
	c.apply()
}

// SetSelection 更新这一行的鼠标选区列范围（原始 rune 列，左闭右开）。
//
// 选区状态存在 CodeView 上，行对象每次被填充时重新领一次 —— List 回收复用
// 行对象、以及滚动/重绘引起的刷新都不会把选区弄丢。
func (c *codeLine) SetSelection(start, end int, ok bool) {
	c.rawSel, c.hasSel = [2]int{start, end}, ok
	c.apply()
}

// apply 统一做横向裁剪，并让查找高亮与选区跟着同一偏移量走。
func (c *codeLine) apply() {
	text, spans, shift := windowTextShift(c.rawText, c.rawSpans)
	c.text, c.spans = text, spans
	c.search = shiftSpans(c.rawSearch, shift, len([]rune(text)))
	if c.hasSel {
		c.sel = shiftSpans([]Span{{StartCol: c.rawSel[0], EndCol: c.rawSel[1]}}, shift, len([]rune(text)))
	} else {
		c.sel = nil
	}
	c.Refresh()
}

// shiftSpans 把一组「原始 rune 列」的范围按 windowTextShift 的裁剪量平移到窗口坐标。
//
// 与 windowTextShift 内部调整漏洞高亮用的是同一套算式；完全落在窗口左侧
// （被 "…" 裁掉）的命中直接丢弃，不留半截色块。
func shiftSpans(spans []Span, shift, textLen int) []Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]Span, 0, len(spans))
	for _, sp := range spans {
		start, end := sp.StartCol, sp.EndCol
		if shift != 0 {
			// 命中整体落在被裁掉的左半部分（"…" 之前）：直接丢弃，不留半截色块。
			if sp.EndCol <= shift+1 {
				continue
			}
			start = sp.StartCol - shift + 1
			end = sp.EndCol - shift + 1
			if start < 2 {
				start = 2 // 不要把 "…" 本身算进高亮
			}
			if start > textLen {
				continue
			}
			if end <= start {
				end = start + 1
			}
			if end > textLen+1 {
				end = textLen + 1
			}
		}
		out = append(out, Span{
			StartCol: start, EndCol: end, Kind: sp.Kind,
			OrigStart: sp.OrigStart, OrigEnd: sp.OrigEnd,
		})
	}
	return out
}

// MinSize 返回行尺寸。
func (c *codeLine) MinSize() fyne.Size {
	return fyne.NewSize(240, lineHeight())
}

// CreateRenderer 实现 fyne.Widget。
func (c *codeLine) CreateRenderer() fyne.WidgetRenderer {
	r := &codeLineRenderer{line: c}
	r.rowBG = canvas.NewRectangle(color.Transparent)
	r.num = widget.NewLabel("")
	r.num.Alignment = fyne.TextAlignTrailing
	r.num.TextStyle = codeStyle()
	r.num.Truncation = fyne.TextTruncateClip
	r.num.Importance = widget.LowImportance
	r.code = widget.NewLabel("")
	r.code.TextStyle = codeStyle()
	r.code.Truncation = fyne.TextTruncateEllipsis
	r.code.Wrapping = fyne.TextWrapOff
	// 代码的「选中复制」不再用 Label 自带的 Selectable：
	// 它只能选中一行之内，而且 List 一刷新就会把选区清掉（见 codeselect.go）。
	// 现在选区由 CodeView 统一维护、由本行画出半透明色块，
	// 鼠标事件由外层 codeViewHost 接收，可以跨行拖拽、滚动后依然保留。
	r.code.Selectable = false

	r.objects = append(r.objects, r.rowBG)
	for i := 0; i < maxSpansPerLine; i++ {
		bg := canvas.NewRectangle(color.Transparent)
		bg.CornerRadius = 3
		r.spanBGs = append(r.spanBGs, bg)
		r.objects = append(r.objects, bg)
	}
	// 查找命中排在漏洞高亮之后：色块是半透明的，叠在红色/黄色高亮上仍能看清两边。
	for i := 0; i < maxSearchSpansPerLine; i++ {
		bg := canvas.NewRectangle(color.Transparent)
		bg.CornerRadius = 3
		r.searchBGs = append(r.searchBGs, bg)
		r.objects = append(r.objects, bg)
	}
	// 选区画在所有高亮之上（但在文字之下），这样即使选中一段沉淀点/来源点，
	// 也能立刻看出「哪一段被选中了」。
	r.selBG = canvas.NewRectangle(color.Transparent)
	r.selBG.CornerRadius = 2
	r.objects = append(r.objects, r.selBG)
	r.objects = append(r.objects, r.num, r.code)
	return r
}

type codeLineRenderer struct {
	line      *codeLine
	objects   []fyne.CanvasObject
	rowBG     *canvas.Rectangle
	spanBGs   []*canvas.Rectangle
	searchBGs []*canvas.Rectangle
	selBG     *canvas.Rectangle
	num       *widget.Label
	code      *widget.Label
}

func (r *codeLineRenderer) Layout(size fyne.Size) {
	c := r.line
	r.rowBG.FillColor = rowColor(c.kind)
	r.rowBG.Move(fyne.NewPos(0, 0))
	r.rowBG.Resize(size)

	if c.lineNo > 0 {
		r.num.SetText(strconv.Itoa(c.lineNo))
	} else {
		r.num.SetText("")
	}
	r.num.Move(fyne.NewPos(0, 0))
	r.num.Resize(fyne.NewSize(gutterWidth-8, size.Height))

	r.code.SetText(c.text)
	r.code.Move(fyne.NewPos(gutterWidth, 0))
	r.code.Resize(fyne.NewSize(size.Width-gutterWidth, size.Height))

	layoutSpanBGs(r.spanBGs, c.text, c.spans, size)
	layoutSpanBGs(r.searchBGs, c.text, c.search, size)
	layoutSpanRect(r.selBG, c.text, c.sel, size, colorSelectSpan)
}

// layoutSpanBGs 把一组列范围画成行内背景色块，颜色由高亮类型决定。
func layoutSpanBGs(bgs []*canvas.Rectangle, text string, spans []Span, size fyne.Size) {
	for i, bg := range bgs {
		if i >= len(spans) {
			bg.Hide()
			continue
		}
		layoutSpanRect(bg, text, spans[i:i+1], size, spanColor(spans[i].Kind))
	}
}

// layoutSpanRect 按列范围摆放色块：spans 为空则隐藏。
//
// 列位置一律用 codeTextSize + codeStyle 测量，并加上 codeTextOriginX 的
// innerPadding 偏移（RichText 真正画字形的起点），与漏洞高亮、列标尺、
// 鼠标选区的列位置共用同一套算法，保证「第 26 列」在四处落在同一个像素上。
func layoutSpanRect(bg *canvas.Rectangle, text string, spans []Span, size fyne.Size, fill color.Color) {
	if len(spans) == 0 {
		bg.Hide()
		return
	}
	sp := spans[0]
	size_ := codeTextSize()
	prefix := sliceRunes(text, 0, sp.StartCol-1)
	mid := sliceRunes(text, sp.StartCol-1, sp.EndCol-1)
	x := codeTextOriginX() + fyne.MeasureText(prefix, size_, codeStyle()).Width
	w := fyne.MeasureText(mid, size_, codeStyle()).Width
	if w < 3 {
		w = 3
	}
	bg.FillColor = fill
	bg.Move(fyne.NewPos(x, 2))
	bg.Resize(fyne.NewSize(w, size.Height-4))
	bg.Show()
}

func (r *codeLineRenderer) MinSize() fyne.Size { return r.line.MinSize() }

func (r *codeLineRenderer) Refresh() {
	r.Layout(r.line.Size())
	canvas.Refresh(r.line)
}

func (r *codeLineRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *codeLineRenderer) Destroy() {}

// CodeView 是带高亮能力的只读代码视图。
type CodeView struct {
	path    string
	encName string
	lines   []string
	hl      map[int]HLKind
	spans   map[int][]Span
	list    *widget.List
	host    *codeViewHost
	// ruler 是顶部列标尺（「第几列」的定位标识），root 是「查找条 + 标尺 + 列表」的组合。
	ruler   *ColumnRuler
	root    *fyne.Container
	rulerOn bool
	// pendingLine 记录「布局还没完成时想居中的那一行」，等窗口完成布局后补做。
	pendingLine int

	// 查找（Cmd/Ctrl+F）相关状态，详见 codesearch.go。
	searchBar   *codeSearchBar
	query       string
	matches     []codeMatch
	matchIdx    int
	searchSpans map[int][]Span

	// 鼠标选区状态（详见 codeselect.go）：锚点是按下处，光标是当前拖动到的地方。
	// 放在 CodeView 上而不是行控件上，才能跨行选中并在列表刷新后保留。
	selAnchor codePos
	selCursor codePos
	// dragSession 记录当前是否处于一次鼠标拖拽中（锚点已确定）。
	dragSession bool
	// onCopy 是「复制成功」的界面反馈（状态栏提示）。
	onCopy func(lines int)
}

// codeViewHost 只是给 List 包一层，用来监听尺寸变化。
//
// 底部补白行数取决于视口高度，窗口大小一变就得重算；而 List 的内容高度是
// 缓存的（minSizeCache），必须调用 List.Refresh 才会失效。Fyne 的 List 没有
// 暴露 Resize 回调，所以在这里包一层。
//
// 顺带承担一件事：如果跳转发生在窗口完成布局之前（视口高度还是 0，例如启动
// 参数直接载入项目、或截图模式），当时算不出偏移量，就在第一次布局完成后补一次
// 「居中滚动」，保证高亮行确实落在正中间。
type codeViewHost struct {
	widget.BaseWidget
	list *widget.List
	view *CodeView
}

func newCodeViewHost(view *CodeView, list *widget.List) *codeViewHost {
	h := &codeViewHost{list: list, view: view}
	h.ExtendBaseWidget(h)
	return h
}

func (h *codeViewHost) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.list)
}

func (h *codeViewHost) Resize(size fyne.Size) {
	before := h.Size()
	h.BaseWidget.Resize(size)
	if before == size {
		return
	}
	h.list.Refresh() // 让补白行数与内容高度按新视口重算
	if h.view != nil {
		h.view.recenterPending()
	}
}

// NewCodeView 创建代码视图。
func NewCodeView() *CodeView {
	cv := &CodeView{
		hl:    map[int]HLKind{},
		spans: map[int][]Span{},
	}
	cv.list = widget.NewList(
		func() int { return cv.totalRows() },
		func() fyne.CanvasObject { return newCodeLine(cv) },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			cl := o.(*codeLine)
			if int(id) >= len(cv.lines) {
				// 底部补白行：只画空白，不显示行号
				cl.Set(0, "", HLNone, nil)
				cl.SetSearch(nil)
				cl.SetSelection(0, 0, false)
				return
			}
			cl.Set(int(id)+1, cv.lines[id], cv.hl[int(id)], cv.spans[int(id)])
			// 查找命中与选区都是独立的一层，必须每次填充都重新给一遍：
			// List 会复用行对象，不给就会被上一条数据的高亮串味。
			cl.SetSearch(cv.searchSpansForLine(int(id)))
			s, e, ok := cv.selectionForLine(int(id) + 1)
			cl.SetSelection(s, e, ok)
		},
	)
	cv.list.Select(0)
	cv.host = newCodeViewHost(cv, cv.list)
	cv.ruler = newColumnRuler()
	cv.ruler.Hide() // 默认隐藏，由 SetColumnRuler 按配置决定是否显示
	cv.searchBar = newCodeSearchBar(cv)
	// 三层：最上面是可开关的查找条，往下是列标尺，最下面是代码列表。
	withRuler := container.NewBorder(cv.ruler, nil, nil, nil, cv.host)
	cv.root = container.NewBorder(cv.searchBar.root, nil, nil, nil, withRuler)
	return cv
}

// SetColumnRuler 打开/关闭顶部列标尺。
//
// 关键点是最后那次 relayout：Fyne 的 Hide/Show 只会重绘控件本身，父容器的
// 布局不会自动重跑，Border 就仍然把顶部那条高度算给标尺，于是关掉开关后
// 代码上方会留下一条什么都没画的空白（用户反馈的「隐藏得不干净」）。
// 主动让「标尺 + 列表」容器重排一次，列表才会立刻顶上去。
func (cv *CodeView) SetColumnRuler(on bool) {
	if cv.ruler == nil || cv.rulerOn == on {
		return
	}
	cv.rulerOn = on
	if on {
		cv.ruler.Show()
	} else {
		cv.ruler.Hide()
		cv.ruler.Set(0, "", nil, 0) // 关掉后不留旧内容，重新开启时按当前行重算
	}
	cv.ruler.Refresh()
	cv.relayout()
	cv.list.Refresh()
}

// relayout 让「标尺 + 代码列表」的容器按当前可见性重新布局。
func (cv *CodeView) relayout() {
	if cv.root != nil {
		cv.root.Refresh()
	}
}

// ColumnRulerOn 返回列标尺当前是否开启。
func (cv *CodeView) ColumnRulerOn() bool { return cv.rulerOn }

// RefreshRuler 按 focusLine 重算列标尺：优先标注聚集点所在行，
// 该行没有高亮时退回到文件里第一处有高亮的行。
func (cv *CodeView) RefreshRuler(focusLine int) {
	if cv.ruler == nil || !cv.rulerOn {
		return
	}
	idx := -1
	if focusLine > 0 && len(cv.spans[focusLine-1]) > 0 {
		idx = focusLine - 1
	} else {
		best := -1
		for ln, sp := range cv.spans {
			if len(sp) == 0 {
				continue
			}
			if best < 0 || ln < best {
				best = ln
			}
		}
		idx = best
	}
	if idx < 0 || idx >= len(cv.lines) {
		cv.ruler.Set(0, "", nil, 0)
		return
	}
	text, spans, shift := windowTextShift(cv.lines[idx], cv.spans[idx])
	cv.ruler.Set(idx+1, text, spans, shift)
}

// padRows 计算底部补白行数。
//
// 目的：让文件末尾的漏洞也能滚到视口正中间。List 的滚动范围被内容高度限制，
// 没有补白时最后一屏永远无法居中（Fyne 会把 offset 夹到 内容高-视口高）。
// 补白只在「文件比视口高」时启用，避免短文件底部出现大片空白。
func (cv *CodeView) padRows() int {
	n := len(cv.lines)
	if n == 0 {
		return 0
	}
	vh := cv.viewportHeight()
	pitch := cv.rowPitch()
	if vh <= 0 || pitch <= 0 {
		return 0
	}
	if float32(n)*pitch <= vh {
		return 0 // 整份文件都装得下，不需要滚动
	}
	pad := int(vh/pitch/2) + 1
	if pad > 100 {
		pad = 100
	}
	return pad
}

// totalRows 是 List 的实际行数（真实行 + 补白行）。
func (cv *CodeView) totalRows() int { return len(cv.lines) + cv.padRows() }

// Object 返回可放置的组件。
func (cv *CodeView) Object() fyne.CanvasObject {
	if cv.root != nil {
		return cv.root
	}
	if cv.host != nil {
		return cv.host
	}
	return cv.list
}

// Path 返回当前文件路径。
func (cv *CodeView) Path() string { return cv.path }

// Encoding 返回探测到的编码名。
func (cv *CodeView) Encoding() string { return cv.encName }

// SetFile 设置要显示的文件内容。
func (cv *CodeView) SetFile(path, encName string, lines []string) {
	cv.path = path
	cv.encName = encName
	cv.lines = lines
	cv.hl = map[int]HLKind{}
	cv.spans = map[int][]Span{}
	cv.pendingLine = 0
	// 换文件后旧选区没有意义（行列都对不上了），必须清掉，
	// 否则「复制选中代码」会把新文件的同几行复制出去。
	cv.selAnchor, cv.selCursor = codePos{}, codePos{}
	cv.researchCurrentFile()
	cv.list.Refresh()
	cv.list.ScrollToTop()
	if cv.ruler != nil {
		cv.ruler.Set(0, "", nil, 0)
	}
}

// researchCurrentFile 换文件后按当前查找词重新查找一遍。
//
// 不滚动：调用方（showAt）随后会把跳转目标行居中，查找不应该抢这次滚动。
func (cv *CodeView) researchCurrentFile() {
	if cv.searchBar == nil || !cv.searchBar.root.Visible() {
		cv.matches, cv.matchIdx, cv.searchSpans = nil, 0, nil
		return
	}
	cv.search(cv.searchBar.entry.Text, cv.searchBar.caseChk.Checked, false)
}

// Clear 清空视图。
func (cv *CodeView) Clear() {
	cv.path, cv.encName, cv.lines = "", "", nil
	cv.hl = map[int]HLKind{}
	cv.spans = map[int][]Span{}
	cv.pendingLine = 0
	cv.selAnchor, cv.selCursor = codePos{}, codePos{}
	cv.matches, cv.matchIdx, cv.searchSpans = nil, 0, nil
	cv.updateSearchStatus()
	cv.list.Refresh()
	if cv.ruler != nil {
		cv.ruler.Set(0, "", nil, 0)
	}
}

// AddRange 添加一段高亮区域（1-based 行列，左闭右开列）。
func (cv *CodeView) AddRange(kind HLKind, startLine, startCol, endLine, endCol int) {
	if startLine <= 0 {
		return
	}
	// 先留一份 CSV 原值：列标尺要按原样显示「第几列」（与证据链一致），
	// 而下面的 EndCol 会被补齐、跨行时会被改成「到行尾」。
	rawStart, rawEnd := startCol, endCol
	if endLine < startLine {
		endLine = startLine
	}
	// CodeQL 的单点位置（start==end）也要能被看见，因此这里统一保证至少高亮一个字符。
	if endCol <= startCol {
		endCol = startCol + 1
	}
	for ln := startLine; ln <= endLine; ln++ {
		idx := ln - 1
		if idx < 0 {
			continue
		}
		cv.hl[idx] = mergeKind(cv.hl[idx], kind)
		var sp Span
		switch {
		case startLine == endLine:
			sp = Span{StartCol: startCol, EndCol: endCol, Kind: kind,
				OrigStart: rawStart, OrigEnd: rawEnd}
		case ln == startLine:
			sp = Span{StartCol: startCol, EndCol: spanToLineEnd, Kind: kind,
				OrigStart: rawStart, OrigEnd: spanToLineEnd}
		case ln == endLine:
			sp = Span{StartCol: 1, EndCol: endCol, Kind: kind,
				OrigStart: 1, OrigEnd: rawEnd}
		default:
			sp = Span{StartCol: 1, EndCol: spanToLineEnd, Kind: kind,
				OrigStart: 1, OrigEnd: spanToLineEnd}
		}
		if sp.StartCol <= 0 {
			sp.StartCol = 1
		}
		if len(cv.spans[idx]) < maxSpansPerLine {
			cv.spans[idx] = append(cv.spans[idx], sp)
		}
	}
}

// ClearHighlight 清除全部高亮。
func (cv *CodeView) ClearHighlight() {
	cv.hl = map[int]HLKind{}
	cv.spans = map[int][]Span{}
	cv.list.Refresh()
}

// Apply 重建高亮并刷新。
func (cv *CodeView) Apply() { cv.list.Refresh() }

// ScrollToLine 滚动到指定行（1-based）。
//
// 这里的语义是「把目标行放到视口垂直中央」：Fyne 的 List.ScrollTo 只在目标
// 不可见时才滚动，而且贴边（目标常常刚好落在视口下沿之外），用户在左侧点一条
// 漏洞后经常看不到高亮行。ScrollToLineCentered 用显式 offset 把它摆到中间。
func (cv *CodeView) ScrollToLine(line int) { cv.ScrollToLineCentered(line) }

// rowPitch 返回相邻两行的垂直间距。
//
// 代码视图的所有行都是同一个模板渲染出来的固定行高，因此
// 「第 N 行的 y 偏移 = (N-1) * rowPitch」是精确的；List 内部也是这个算法
// （itemMin.Height + Theme 的 padding 作为分隔条厚度）。
func (cv *CodeView) rowPitch() float32 {
	pitch := lineHeight() + theme.Current().Size(theme.SizeNamePadding)
	if pitch <= 0 {
		pitch = lineHeight()
	}
	return pitch
}

// LineHeight 返回单行内容高度（不含分隔条），供测试与布局计算使用。
func (cv *CodeView) LineHeight() float32 { return lineHeight() }

// LineCount 返回当前文件的行数。
func (cv *CodeView) LineCount() int { return len(cv.lines) }

// viewportHeight 返回代码视图可视区域高度。
func (cv *CodeView) viewportHeight() float32 {
	if h := cv.list.Size().Height; h > 0 {
		return h
	}
	if cv.host != nil {
		return cv.host.Size().Height
	}
	return 0
}

// ViewportHeight 返回代码视图可视区域高度。
func (cv *CodeView) ViewportHeight() float32 { return cv.viewportHeight() }

// ContentHeight 返回全部行（含底部补白）的总高度。
func (cv *CodeView) ContentHeight() float32 {
	if n := cv.totalRows(); n > 0 {
		return float32(n)*cv.rowPitch() - theme.Current().Size(theme.SizeNamePadding)
	}
	return 0
}

// RealContentHeight 返回不含补白的真实内容高度。
func (cv *CodeView) RealContentHeight() float32 {
	if len(cv.lines) == 0 {
		return 0
	}
	return float32(len(cv.lines))*cv.rowPitch() - theme.Current().Size(theme.SizeNamePadding)
}

// ScrollOffset 返回当前滚动偏移（供测试断言）。
func (cv *CodeView) ScrollOffset() float32 { return cv.list.GetScrollOffset() }

// ScrollToLineCentered 滚动到指定行（1-based），并让它尽量落在视口垂直中央。
//
// 计算流程：目标行顶部 y → 减去半个视口（再加上半行，让行自身居中）→ 夹到
// [0, 内容高-视口高]。最后用 List.ScrollToOffset 显式设定偏移，避免 Fyne 的
// 「最小滚动」策略把高亮行留在视口边缘之外。
func (cv *CodeView) ScrollToLineCentered(line int) {
	if len(cv.lines) == 0 {
		return
	}
	idx := line - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cv.lines) {
		idx = len(cv.lines) - 1
	}
	pitch := cv.rowPitch()
	viewport := cv.ViewportHeight()
	if viewport <= 0 {
		// 还没有完成布局（启动参数载入、截图模式等）：先记下来，
		// 等 codeViewHost 收到第一次 Resize 时补做居中。
		cv.pendingLine = idx + 1
		cv.list.ScrollTo(widget.ListItemID(idx))
		return
	}
	cv.pendingLine = 0
	target := float32(idx)*pitch - (viewport-pitch)/2
	if target < 0 {
		target = 0
	}
	if maxOff := cv.ContentHeight() - viewport; maxOff > 0 && target > maxOff {
		target = maxOff
	}
	cv.list.ScrollToOffset(target)
	cv.list.Refresh()
}

// recenterPending 在窗口完成布局后补做一次居中（见 codeViewHost.Resize）。
func (cv *CodeView) recenterPending() {
	line := cv.pendingLine
	if line <= 0 || len(cv.lines) == 0 || cv.ViewportHeight() <= 0 {
		return
	}
	cv.pendingLine = 0
	cv.ScrollToLineCentered(line)
}

// Summary 返回统计信息，用于状态栏。
func (cv *CodeView) Summary() string {
	if cv.path == "" {
		return "未打开文件"
	}
	return fmt.Sprintf("%s（%d 行，编码 %s）", shortPath(cv.path), len(cv.lines), cv.encName)
}

func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) <= 3 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-3:], "/")
}
