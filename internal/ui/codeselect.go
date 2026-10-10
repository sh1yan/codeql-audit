package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// 本文件实现代码视图的「鼠标选区 + 复制」。
//
// 为什么不用 Fyne 自带的 widget.Label.Selectable：
//
//  1. 代码视图是 widget.List，每一行代码是独立的 Label，而 Label 的选区
//     只存在于自己那一行之内 —— 用户永远没法选中一整段代码抄进报告；
//  2. List 只要刷新（滚动、换文件、查找、窗口缩放）就会用 SetText 重新填内容，
//     而 SetText 会把 Label 内部的选区清空 —— 刚拖出来的选区一松手就可能没了。
//
// 所以选区状态放在 CodeView 上（与 List 的行对象无关），行对象每次被填充时
// 领到「本行的选区列范围」并自己画一层半透明色块；鼠标事件挂在行控件
// codeLine 上（原因见下面「鼠标 / 快捷键入口」那段注释），按画布绝对坐标
// 换算行列，因此可以跨行、可以在列表刷新与滚动之后依然保留。
// 复制走 Fyne 的快捷键分发（Cmd/Ctrl+C → 当前聚焦对象），右键菜单里也有同样的入口。

// codePos 是代码视图里的一个位置。
//
// line 为 1-based 行号；col 为「原始 rune 列」（1-based），与 Span 使用同一套
// 坐标 —— 即横向裁剪之前的列号，渲染时再按 windowTextShift 的偏移量平移。
type codePos struct {
	line int
	col  int
}

func (p codePos) before(o codePos) bool {
	if p.line != o.line {
		return p.line < o.line
	}
	return p.col < o.col
}

func (p codePos) same(o codePos) bool { return p.line == o.line && p.col == o.col }

// codeTextOriginX 是行内代码文本的像素起点。
//
// 行号栏（gutterWidth）之后，Label 里的 RichText 还会再留一个 innerPadding
// （默认 8px）才画第一个字形。行内高亮色块、列标尺色带、选区色块、鼠标列换算
// 必须共用这个起点，否则会整体左移一个字符宽 —— 看起来就是「高亮/选中的是前一列」。
func codeTextOriginX() float32 {
	return float32(gutterWidth) + theme.Current().Size(theme.SizeNameInnerPadding)
}

// dispColToRaw 把「窗口化文本里的列」还原成原始 rune 列。
//
// windowTextShift 裁剪长行时会在左边补一个 "…"（占 1 个 rune），因此窗口里的
// 第 2 列才是原始的第 shift+1 列；窗口第 1 列（"…"）按原始第 shift+1 列处理，
// 也就是「点到省略号就当作点在第一个可见字符上」。
func dispColToRaw(disp, shift int) int {
	if shift <= 0 {
		return disp
	}
	if disp <= 1 {
		return shift + 1
	}
	return disp + shift - 1
}

// ---------- 选区状态 ----------

// beginSelection 把光标放到 pos，并清空旧选区（等同于「按下鼠标」）。
func (cv *CodeView) beginSelection(pos codePos) {
	cv.selAnchor, cv.selCursor = pos, pos
	cv.refreshSelection()
}

// extendSelection 把选区的另一端拖到 pos（锚点不动）。
func (cv *CodeView) extendSelection(pos codePos) {
	cv.selCursor = pos
	cv.refreshSelection()
}

// SelectAll 全选整个文件。
func (cv *CodeView) SelectAll() {
	if len(cv.lines) == 0 {
		return
	}
	last := len(cv.lines)
	cv.selAnchor = codePos{line: 1, col: 1}
	cv.selCursor = codePos{line: last, col: len([]rune(cv.lines[last-1])) + 1}
	cv.refreshSelection()
}

// HasSelection 返回当前是否有非空选区。
func (cv *CodeView) HasSelection() bool {
	_, _, ok := cv.selBounds()
	return ok
}

// selBounds 返回规范化后的选区（start ≤ end）。
//
// ok 为 false 表示没有选区：锚点与光标重合，或落在最后一行第 1 列上
// （此时选中的内容为空，不该画色块）。
func (cv *CodeView) selBounds() (codePos, codePos, bool) {
	a, b := cv.selAnchor, cv.selCursor
	if a.same(b) || a.line <= 0 || b.line <= 0 {
		return a, b, false
	}
	if b.before(a) {
		a, b = b, a
	}
	if a.line == b.line && a.col >= b.col {
		return a, b, false
	}
	return a, b, true
}

// selectionForLine 返回某一行需要画出的选区列范围（原始 rune 列，左闭右开）。
func (cv *CodeView) selectionForLine(line int) (start, end int, ok bool) {
	if line < 1 || line > len(cv.lines) {
		return 0, 0, false
	}
	lo, hi, has := cv.selBounds()
	if !has || line < lo.line || line > hi.line {
		return 0, 0, false
	}
	n := len([]rune(cv.lines[line-1]))
	start, end = 1, n+1
	if line == lo.line {
		start = lo.col
	}
	if line == hi.line {
		end = hi.col
	}
	if start < 1 {
		start = 1
	}
	if end > n+1 {
		end = n + 1
	}
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

// refreshSelection 让列表按最新选区重画（行对象会重新领到本行的列范围）。
func (cv *CodeView) refreshSelection() {
	if cv.list != nil {
		cv.list.Refresh()
	}
}

// ---------- 选区文本 ----------

// SelectedText 返回当前选中的代码（无选区时为空串）。
func (cv *CodeView) SelectedText() string {
	lo, hi, ok := cv.selBounds()
	if !ok {
		return ""
	}
	return cv.textBetween(lo, hi)
}

// textBetween 按原始行文本拼出 [lo, hi) 之间的内容（多行用 \n 连接）。
//
// 注意：这里取的是「原始行文本」，不受 maxDisplayRunes 横向裁剪影响 ——
// 复制出去的必须是完整代码，而不是屏幕上被省略号截断的那一段。
func (cv *CodeView) textBetween(lo, hi codePos) string {
	if lo.line < 1 {
		lo.line = 1
	}
	if hi.line > len(cv.lines) {
		hi.line = len(cv.lines)
	}
	var b strings.Builder
	for line := lo.line; line <= hi.line; line++ {
		rs := []rune(cv.lines[line-1])
		s, e := 1, len(rs)+1
		if line == lo.line {
			s = lo.col
		}
		if line == hi.line {
			e = hi.col
		}
		if s < 1 {
			s = 1
		}
		if e > len(rs)+1 {
			e = len(rs) + 1
		}
		if e < s {
			e = s
		}
		if line > lo.line {
			b.WriteString("\n")
		}
		b.WriteString(string(rs[s-1 : e-1]))
	}
	return b.String()
}

// ClipboardText 返回「此刻该复制的内容」：
// 有选区就复制选区；没有选区就退化成复制光标所在的整行（方便整行抄代码）。
func (cv *CodeView) ClipboardText() (text string, lines int, ok bool) {
	if lo, hi, has := cv.selBounds(); has {
		return cv.textBetween(lo, hi), hi.line - lo.line + 1, true
	}
	line := cv.selCursor.line
	if line < 1 || line > len(cv.lines) {
		return "", 0, false
	}
	return cv.lines[line-1], 1, true
}

// copySelection 把当前选区（或光标行）写进剪贴板，并通知界面更新状态栏。
func (cv *CodeView) copySelection(cb fyne.Clipboard) bool {
	text, lines, ok := cv.ClipboardText()
	if !ok {
		return false
	}
	if cb == nil {
		cb = fyne.CurrentApp().Clipboard()
	}
	if cb != nil {
		cb.SetContent(text)
	}
	if cv.onCopy != nil {
		cv.onCopy(lines)
	}
	return true
}

// SetOnCopy 注册「复制成功」回调（状态栏提示用）。
func (cv *CodeView) SetOnCopy(fn func(lines int)) { cv.onCopy = fn }

// ---------- 坐标换算 ----------

// lineAtY 把「相对代码列表顶部的 y 像素」换算成行号（1-based）。
//
// 列表的滚动偏移要加回来：y 是视口坐标，而行的位置是内容坐标。
func (cv *CodeView) lineAtY(y float32) int {
	n := len(cv.lines)
	if n == 0 {
		return 0
	}
	pitch := cv.rowPitch()
	if pitch <= 0 {
		return 1
	}
	off := float32(0)
	if cv.list != nil {
		off = cv.list.GetScrollOffset()
	}
	idx := int((y + off) / pitch)
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx + 1
}

// colAtX 把「行内文本区左边界起算的 x 像素」换算成原始 rune 列。
//
// 逐字累计字宽（等宽字体下与 MeasureText(前缀) 等价），落在某个字符的左半边
// 就算它前面的插入点 —— 与常见编辑器的点选手感一致。
func (cv *CodeView) colAtX(line int, x float32) int {
	if line < 1 || line > len(cv.lines) {
		return 1
	}
	raw := cv.lines[line-1]
	rawLen := len([]rune(raw))
	x -= codeTextOriginX()
	if x <= 0 {
		return 1
	}
	// 必须带上这一行的高亮范围：长行只有在「高亮列很靠右」时才会被横向裁剪
	// （左边丢掉一截、补一个 "…"）。这里要用与行渲染完全相同的裁剪量，
	// 否则鼠标位置会和屏幕上的列对不上。
	text, _, shift := windowTextShift(raw, cv.spans[line-1])
	rs := []rune(text)
	size := codeTextSize()
	style := codeStyle()
	var w float32
	for i, r := range rs {
		rw := fyne.MeasureText(string(r), size, style).Width
		if x < w+rw/2 {
			return clampCol(dispColToRaw(i+1, shift), rawLen)
		}
		w += rw
	}
	return clampCol(dispColToRaw(len(rs)+1, shift), rawLen)
}

func clampCol(col, rawLen int) int {
	if col < 1 {
		return 1
	}
	if col > rawLen+1 {
		return rawLen + 1
	}
	return col
}

// posAt 把「相对代码列表的坐标」换算成行列位置。
func (cv *CodeView) posAt(p fyne.Position) codePos {
	line := cv.lineAtY(p.Y)
	return codePos{line: line, col: cv.colAtX(line, p.X)}
}

// posInSelection 判断某个位置是否落在当前选区内（含首尾边界）。
func (cv *CodeView) posInSelection(p codePos) bool {
	lo, hi, ok := cv.selBounds()
	if !ok {
		return false
	}
	return !p.before(lo) && !hi.before(p)
}

// ---------- 拖拽自动滚动 ----------

// autoScroll 在拖拽出视口时按行滚动一格，让「拖到边缘继续选」可用。
//
// 没有用定时器：Fyne 的拖拽事件随鼠标移动而来，鼠标停在边缘不动时就停下，
// 这已经能覆盖「选一大段代码」的实际用法，实现也简单。
func (cv *CodeView) autoScroll(y float32) {
	if cv.list == nil || cv.host == nil {
		return
	}
	h := cv.host.Size().Height
	pitch := cv.rowPitch()
	if h <= 0 || pitch <= 0 {
		return
	}
	off := cv.list.GetScrollOffset()
	switch {
	case y < 0:
		off -= pitch
	case y > h:
		off += pitch
	default:
		return
	}
	if off < 0 {
		off = 0
	}
	if maxOff := cv.ContentHeight() - h; maxOff > 0 && off > maxOff {
		off = maxOff
	}
	cv.list.ScrollToOffset(off)
}

// ---------- 鼠标 / 快捷键入口 ----------

// 鼠标事件为什么挂在「行控件」上，而不是 List 外面那一层 host：
//
// Fyne 的 GLFW 驱动做命中判定时，会沿着渲染树找「最深的、实现了任一鼠标接口」
// 的对象（见 internal/driver/util.go 的 WalkVisibleObjectTree + window.go 的
// processMouseClicked）。List 会为每个条目套一层 *widget.listItem，它实现了
// fyne.Tappable，比 host 更深，因此 host 上的 MouseDown 永远收不到事件
// （早先的实测结论：命中对象是 *widget.listItem）。
//
// 行控件 codeLine 是 listItem 的子对象，比它更深，所以把鼠标接口放在这里。
// 拖拽过程本身不用担心「换行就断了」：驱动在拖拽开始时锁定 Draggable 对象
// （w.mouseDragged），后续事件一直发给同一个对象，并用 AbsolutePosition
// 带上画布绝对坐标 —— 因此跨行、滚动、行对象被回收复用都不影响换算。
var (
	_ desktop.Mouseable      = (*codeLine)(nil)
	_ fyne.Draggable         = (*codeLine)(nil)
	_ fyne.SecondaryTappable = (*codeLine)(nil)
	_ fyne.Shortcutable      = (*codeViewHost)(nil)
	_ fyne.Focusable         = (*codeViewHost)(nil)
)

// absPoint 把一次鼠标事件换算成画布绝对坐标。
//
// Fyne 的真实驱动（glfw）都会填 AbsolutePosition；测试辅助函数 test.Drag
// 不填，这时退回「控件自身位置 + 控件内相对坐标」。
func (c *codeLine) absPoint(p, abs fyne.Position) fyne.Position {
	if abs != (fyne.Position{}) {
		return abs
	}
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(c).Add(p)
}

// MouseDown 开始一次新的选区（锚点 = 光标 = 按下位置）。
func (c *codeLine) MouseDown(e *desktop.MouseEvent) {
	cv := c.view
	if cv == nil || e.Button != desktop.MouseButtonPrimary {
		return // 右键交给 TappedSecondary
	}
	cv.focusCode()
	cv.dragSession = true
	cv.beginSelection(cv.posAtAbs(c.absPoint(e.Position, e.AbsolutePosition)))
}

// MouseUp 结束本次拖拽（选区保留；单击时选区本来就是空的）。
func (c *codeLine) MouseUp(*desktop.MouseEvent) {
	if c.view != nil {
		c.view.dragSession = false
	}
}

// Dragged 扩展选区，并在拖出视口时自动滚动。
func (c *codeLine) Dragged(e *fyne.DragEvent) {
	cv := c.view
	if cv == nil {
		return
	}
	cur := c.absPoint(e.Position, e.AbsolutePosition)
	if !cv.dragSession {
		// 没有收到 MouseDown 就来了拖拽（测试辅助函数、或驱动丢事件）：
		// 用「当前位置 - 本次位移」补出按下点，保证拖拽总能选上。
		cv.dragSession = true
		cv.beginSelection(cv.posAtAbs(cur.Subtract(e.Dragged)))
	}
	cv.extendSelection(cv.posAtAbs(cur))
	cv.autoScroll(cv.hostPoint(cur).Y)
}

// DragEnd 结束拖拽（选区保留）。
func (c *codeLine) DragEnd() {
	if c.view != nil {
		c.view.dragSession = false
	}
}

// TappedSecondary 在右键处弹出复制菜单。
func (c *codeLine) TappedSecondary(e *fyne.PointEvent) {
	cv := c.view
	if cv == nil {
		return
	}
	canvas := fyne.CurrentApp().Driver().CanvasForObject(c)
	if canvas == nil {
		return
	}
	pos := cv.posAtAbs(c.absPoint(e.Position, e.AbsolutePosition))
	if !cv.posInSelection(pos) {
		// 在选区之外右键：把光标移过去并放弃旧选区（与常见编辑器一致）；
		// 在选区内右键则保留选区，菜单里的「复制选中代码」保持可用。
		cv.beginSelection(pos)
	}
	cv.focusCode()
	widget.ShowPopUpMenuAtPosition(cv.contextMenu(), canvas, e.AbsolutePosition)
}

// TypedShortcut 处理 Cmd/Ctrl+C 与 Cmd/Ctrl+A（焦点在代码区 host 上）。
func (h *codeViewHost) TypedShortcut(sh fyne.Shortcut) {
	if h.view == nil {
		return
	}
	switch sc := sh.(type) {
	case *fyne.ShortcutCopy:
		h.view.copySelection(sc.Clipboard)
	case *fyne.ShortcutSelectAll:
		h.view.SelectAll()
	}
}

// FocusGained / FocusLost 让代码区可以成为焦点对象（Cmd/Ctrl+C 的落点）。
func (h *codeViewHost) FocusGained() {}

// FocusLost 目前不需要额外处理：选区不随失焦消失。
func (h *codeViewHost) FocusLost() {}

// TypedRune / TypedKey 是 fyne.Focusable 的一部分：
// 代码区是只读视图，不吞任何字符输入。
func (h *codeViewHost) TypedRune(rune) {}

func (h *codeViewHost) TypedKey(*fyne.KeyEvent) {}

// focusCode 把焦点交给代码区，这样 Cmd/Ctrl+C 才会送到这里。
//
// 焦点给 host（整块代码区共用一个稳定对象），而不是给某一行：
// 行控件会被 List 回收复用，而且每行都实现 Focusable 会让 Tab 键在
// 几十行代码之间逐个跳。
func (cv *CodeView) focusCode() {
	if cv.host == nil {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(cv.host); c != nil {
		c.Focus(cv.host)
	}
}

// hostPoint 把画布绝对坐标换算成「相对代码列表左上角」的坐标。
func (cv *CodeView) hostPoint(abs fyne.Position) fyne.Position {
	if cv.host == nil {
		return abs
	}
	return abs.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(cv.host))
}

// posAtAbs 把画布绝对坐标换算成行列位置。
func (cv *CodeView) posAtAbs(abs fyne.Position) codePos {
	return cv.posAt(cv.hostPoint(abs))
}

// contextMenu 组装代码区的右键菜单。
func (cv *CodeView) contextMenu() *fyne.Menu {
	copySel := fyne.NewMenuItem("复制选中代码", func() { cv.copySelection(nil) })
	copySel.Disabled = !cv.HasSelection()

	copyLine := fyne.NewMenuItem("复制当前行", func() {
		line := cv.selCursor.line
		if line < 1 || line > len(cv.lines) {
			return
		}
		if cb := fyne.CurrentApp().Clipboard(); cb != nil {
			cb.SetContent(cv.lines[line-1])
		}
		if cv.onCopy != nil {
			cv.onCopy(1)
		}
	})

	return fyne.NewMenu("",
		copySel,
		copyLine,
		fyne.NewMenuItem("全选代码", func() { cv.SelectAll() }),
	)
}
