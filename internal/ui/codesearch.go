package ui

import (
	"fmt"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// 本文件实现「在当前代码文件中查找」：工具条 + 命中高亮 + 上下跳转。
//
// 为什么把查找条放进 CodeView 而不是主界面：命中高亮要和漏洞高亮共用同一套
// 列换算与横向裁剪，跳转要和列表的滚动逻辑共用同一份偏移量计算。放在一起，
// 「找到了就一定会滚过去、没找到就绝不高亮」才有保证。
//
// 打开方式：快捷键（默认 Cmd/Ctrl+F，可在设置里改）、顶栏「查找代码」按钮、
// 「编辑」菜单里的「在当前文件中查找」——三处入口都调用 App.openCodeSearch。

// codeMatch 是一处查找命中。
//
// line 是 0-based 行号（与 CodeView.lines 对齐）；列号是 1-based 的 rune 列、
// 右开区间，和漏洞高亮 Span 的列语义完全一致。
type codeMatch struct {
	line     int
	startCol int
	endCol   int
}

const (
	// maxSearchMatches 是一次查找记录的最大命中数。
	//
	// 超长文件里搜「e」这种单字符会命中上万处，既没有阅读价值又白占内存，
	// 因此到上限就停止查找，并在状态里写明「只列出前 N 处」。
	maxSearchMatches = 2000
	// maxSearchSpansPerLine 是单行最多画出的命中背景块数。
	maxSearchSpansPerLine = 8
)

// searchEntry 是一个能响应 Esc 的输入框。
//
// 为什么要包一层：widget.Entry 只暴露 OnSubmitted（回车），没有 Esc 回调；
// 而 Esc 不带修饰键，没法注册成画布快捷键（Fyne 只把「有修饰键 + 主键」的
// 按键事件转成 CustomShortcut）。这里把 Entry 内嵌进来，并用
// ExtendBaseWidget(自己) 把「画布焦点对象」指向外层：驱动会把键盘事件送到
// 焦点对象的 TypedKey，于是我们能在外层拦下 Esc 再交给 Entry 处理其余按键。
type searchEntry struct {
	widget.Entry
	onEscape func()
}

func newSearchEntry() *searchEntry {
	e := &searchEntry{}
	// 与 widget.NewEntry() 保持一致的单行行为。
	e.Entry.Wrapping = fyne.TextWrap(fyne.TextTruncateClip)
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey 拦下 Esc（关闭查找条），其余按键照常交给 Entry。
func (e *searchEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && e.onEscape != nil {
		e.onEscape()
		return
	}
	e.Entry.TypedKey(ev)
}

// focus 把画布焦点交给外层对象（而不是内嵌的 Entry），这样 Esc 才会经过 TypedKey。
func (e *searchEntry) focus() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
		c.Focus(e)
	}
}

// codeSearchBar 是代码视图顶部的查找工具条。
type codeSearchBar struct {
	root    *fyne.Container
	entry   *searchEntry
	status  *widget.Label
	caseChk *widget.Check
}

// newCodeSearchBar 组装查找工具条（默认隐藏）。
func newCodeSearchBar(cv *CodeView) *codeSearchBar {
	b := &codeSearchBar{}
	b.entry = newSearchEntry()
	b.entry.SetPlaceHolder("在当前文件中查找…")
	b.entry.OnChanged = func(string) { cv.runSearch() }
	// 回车 = 跳到下一处。Fyne 的 OnSubmitted 回调里拿不到修饰键（没法区分 Shift+回车），
	// 所以「上一处」只提供按钮入口，不做键位绑定，避免猜错用户意图。
	b.entry.OnSubmitted = func(string) { cv.NextMatch() }
	b.entry.onEscape = func() { cv.CloseSearch() }

	b.status = widget.NewLabel("")
	b.status.Importance = widget.LowImportance
	b.caseChk = widget.NewCheck("区分大小写", func(bool) { cv.runSearch() })

	prev := widget.NewButtonWithIcon("上一处", theme.NavigateBackIcon(), func() { cv.PrevMatch() })
	next := widget.NewButtonWithIcon("下一处", theme.NavigateNextIcon(), func() { cv.NextMatch() })
	closeBtn := widget.NewButtonWithIcon("关闭", theme.CancelIcon(), func() { cv.CloseSearch() })
	closeBtn.Importance = widget.LowImportance

	body := container.NewBorder(nil, nil, nil,
		container.NewHBox(b.caseChk, b.status, prev, next, closeBtn), b.entry)
	b.root = container.NewVBox(container.NewPadded(body), widget.NewSeparator())
	b.root.Hide()
	return b
}

// Status 返回状态文案（供测试断言）。
func (b *codeSearchBar) Status() string { return b.status.Text }

// ------------------------- CodeView 上的查找逻辑 -------------------------

// OpenSearch 打开查找工具条并把键盘焦点交给输入框。
//
// 已经打开时再次触发（例如又按了一次 Cmd+F）：不重复建控件，只把焦点收回来，
// 并按当前输入重新查找一次 —— 用户按 Cmd+F 的意图就是「我要找东西」。
func (cv *CodeView) OpenSearch() {
	if cv.searchBar == nil {
		return
	}
	cv.searchBar.root.Show()
	cv.relayout()
	cv.searchBar.entry.focus()
	if strings.TrimSpace(cv.searchBar.entry.Text) != "" {
		cv.search(cv.searchBar.entry.Text, cv.searchBar.caseChk.Checked, true)
	} else {
		cv.updateSearchStatus()
	}
}

// CloseSearch 收起查找工具条并清除查找高亮（不动漏洞高亮）。
func (cv *CodeView) CloseSearch() {
	if cv.searchBar == nil || !cv.searchBar.root.Visible() {
		return
	}
	cv.searchBar.root.Hide()
	cv.matches = nil
	cv.matchIdx = 0
	cv.searchSpans = nil
	cv.list.Refresh()
	cv.relayout()
	cv.releaseSearchFocus()
}

// releaseSearchFocus 把键盘焦点从（已经隐藏的）查找输入框上收回来。
//
// 关键：输入框实现了 Shortcutable，如果焦点留在隐藏的输入框上，之后按
// Cmd/Ctrl+C 之类会被它吃掉，用户会觉得「快捷键突然失灵」。Esc 关闭后
// 焦点交还给画布（代码区没有可聚焦的控件，取消聚焦即可）。
func (cv *CodeView) releaseSearchFocus() {
	if cv.searchBar == nil {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(cv.searchBar.entry); c != nil {
		if c.Focused() == cv.searchBar.entry {
			c.Unfocus()
		}
	}
}

// SearchBarVisible 返回查找工具条当前是否显示。
func (cv *CodeView) SearchBarVisible() bool {
	return cv.searchBar != nil && cv.searchBar.root.Visible()
}

// SetSearchHint 更新输入框里的提示文案（把当前生效的快捷键显示出来）。
func (cv *CodeView) SetSearchHint(shortcut string) {
	if cv.searchBar == nil {
		return
	}
	if strings.TrimSpace(shortcut) == "" {
		cv.searchBar.entry.SetPlaceHolder("在当前文件中查找…")
		return
	}
	cv.searchBar.entry.SetPlaceHolder(fmt.Sprintf("在当前文件中查找…（%s）", shortcut))
}

// SearchQuery 返回当前的查找词。
func (cv *CodeView) SearchQuery() string { return cv.query }

// MatchCount 返回当前文件里的命中总数。
func (cv *CodeView) MatchCount() int { return len(cv.matches) }

// CurrentMatch 返回当前命中序号（1-based；没有命中时返回 0）。
func (cv *CodeView) CurrentMatch() int {
	if len(cv.matches) == 0 {
		return 0
	}
	return cv.matchIdx + 1
}

// MatchLine 返回第 i 处命中所在行（1-based，i 从 0 开始；越界返回 0）。
func (cv *CodeView) MatchLine(i int) int {
	if i < 0 || i >= len(cv.matches) {
		return 0
	}
	return cv.matches[i].line + 1
}

// SearchStatus 返回查找条上的状态文案。
func (cv *CodeView) SearchStatus() string {
	if cv.searchBar == nil {
		return ""
	}
	return cv.searchBar.Status()
}

// RunSearch 按输入框当前内容执行一次查找（供测试与外部触发使用）。
func (cv *CodeView) RunSearch() { cv.runSearch() }

// runSearch 由输入框的内容变化/大小写开关触发。
func (cv *CodeView) runSearch() {
	if cv.searchBar == nil {
		return
	}
	cv.search(cv.searchBar.entry.Text, cv.searchBar.caseChk.Checked, true)
}

// NextMatch 跳到下一处命中（循环）。
func (cv *CodeView) NextMatch() { cv.stepMatch(1) }

// PrevMatch 跳到上一处命中（循环）。
func (cv *CodeView) PrevMatch() { cv.stepMatch(-1) }

// stepMatch 在命中列表里前后移动并滚动到目标行。
func (cv *CodeView) stepMatch(delta int) {
	n := len(cv.matches)
	if n == 0 {
		cv.updateSearchStatus()
		return
	}
	cv.matchIdx = (cv.matchIdx + delta + n) % n
	cv.rebuildSearchSpans()
	cv.list.Refresh()
	cv.updateSearchStatus()
	cv.ScrollToLineCentered(cv.matches[cv.matchIdx].line + 1)
}

// search 是查找的唯一入口：重新计算命中、重建高亮、更新状态，必要时滚动。
func (cv *CodeView) search(query string, caseSensitive, scroll bool) {
	cv.query = query
	cv.matches = cv.findMatches(query, caseSensitive)
	cv.matchIdx = 0
	cv.rebuildSearchSpans()
	cv.list.Refresh()
	cv.updateSearchStatus()
	if scroll && len(cv.matches) > 0 {
		cv.ScrollToLineCentered(cv.matches[0].line + 1)
	}
}

// findMatches 在整份文件里查找 query，返回按「行号升序、行内列号升序」排列的命中。
//
// 大小写不敏感时用 unicode.ToLower 逐 rune 比较：不能用 strings.ToLower 后按字节
// 定位，那会让某些字符转换后长度变化，列号（rune 列）随即错位。
func (cv *CodeView) findMatches(query string, caseSensitive bool) []codeMatch {
	// 只有空白字符不算查找：否则「空格」会把每一行都点亮，而状态栏还写着「输入关键字…」。
	if strings.TrimSpace(query) == "" {
		return nil
	}
	q := []rune(query)
	var out []codeMatch
	for i, line := range cv.lines {
		if len(out) >= maxSearchMatches {
			break
		}
		rs := []rune(line)
		for col := 0; col+len(q) <= len(rs); {
			if runesEqual(rs[col:col+len(q)], q, caseSensitive) {
				out = append(out, codeMatch{line: i, startCol: col + 1, endCol: col + len(q) + 1})
				if len(out) >= maxSearchMatches {
					break
				}
				col += len(q)
				continue
			}
			col++
		}
	}
	return out
}

// runesEqual 比较两段 rune 是否相同（可按需忽略大小写）。
func runesEqual(a, b []rune, caseSensitive bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if !caseSensitive {
			x, y = unicode.ToLower(x), unicode.ToLower(y)
		}
		if x != y {
			return false
		}
	}
	return true
}

// rebuildSearchSpans 依据命中列表重建「行号 → 命中列范围」。
//
// 当前命中（琥珀色）始终先登记，避免一行里命中过多触及上限时把它挤掉。
func (cv *CodeView) rebuildSearchSpans() {
	cv.searchSpans = nil
	if len(cv.matches) == 0 {
		return
	}
	spans := make(map[int][]Span, len(cv.matches))
	add := func(m codeMatch, kind HLKind) {
		if len(spans[m.line]) >= maxSearchSpansPerLine {
			return
		}
		spans[m.line] = append(spans[m.line],
			Span{StartCol: m.startCol, EndCol: m.endCol, Kind: kind,
				OrigStart: m.startCol, OrigEnd: m.endCol})
	}
	if cv.matchIdx >= 0 && cv.matchIdx < len(cv.matches) {
		add(cv.matches[cv.matchIdx], HLSearchActive)
	}
	for i, m := range cv.matches {
		if i == cv.matchIdx {
			continue
		}
		add(m, HLSearch)
	}
	cv.searchSpans = spans
}

// updateSearchStatus 刷新查找条上的「第几处 / 共几处」。
func (cv *CodeView) updateSearchStatus() {
	if cv.searchBar == nil {
		return
	}
	switch {
	case strings.TrimSpace(cv.query) == "":
		cv.searchBar.status.SetText("输入关键字后自动高亮，回车跳到下一处")
	case len(cv.matches) == 0:
		cv.searchBar.status.SetText("无匹配")
	case len(cv.matches) >= maxSearchMatches:
		cv.searchBar.status.SetText(fmt.Sprintf("第 %d / %d 处（只列出前 %d 处）",
			cv.matchIdx+1, len(cv.matches), maxSearchMatches))
	default:
		cv.searchBar.status.SetText(fmt.Sprintf("第 %d / %d 处", cv.matchIdx+1, len(cv.matches)))
	}
}

// searchSpansForLine 返回某一行（0-based）要画的查找高亮。
func (cv *CodeView) searchSpansForLine(idx int) []Span {
	if cv.searchSpans == nil {
		return nil
	}
	return cv.searchSpans[idx]
}
