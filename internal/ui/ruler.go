package ui

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ColumnRuler 是代码视图顶部的「列标尺」。
//
// 右栏证据链里写着「第 N 行 · 列 26-28」，但中栏代码本身只有行号，用户没法
// 一眼看出高亮的那一段究竟停在第几列。这里把列号直接画在代码上方：
//
//   - 下半部是色带与刻度线：把当前聚焦的沉淀点/来源点列范围画成色带，每 10 列
//     一根刻度线；
//   - 上半部是刻度数字：每 10 列标一个列号，左端的「列」字对齐行号栏；
//   - 列号色块：在色带旁边贴一块与沉淀点/来源点同色的小标签「列 26-28」，
//     并用一根竖线指向色带中心。
//
// 参考行取「当前聚焦的那一行」（跳转到的沉淀点/来源点所在行），并且用与
// codeLine 完全相同的 windowText 规则横向裁剪，保证标尺与代码的列位置严格对齐
// （列像素一律用 codeTextSize + codeStyle 测量，与行内高亮同一套算法）。
//
// 该组件由「设置 → 代码视图 → 显示列标尺」控制显隐，见 CodeView.SetColumnRuler。
type ColumnRuler struct {
	widget.BaseWidget

	lineNo int    // 参考行行号（1-based，0 表示没有可标注的行）
	text   string // 参考行文本（已按高亮窗口裁剪）
	spans  []Span // 参考行上的高亮列范围（已随文本一起调整过）
	shift  int    // 文本左侧被裁掉的 rune 数，用于把窗口列号还原成原始列号
}

const (
	// rulerHeight 是列标尺的总高度。
	//
	// 旧版是 34px（上下两行分离：上行定位标识、下行刻度），在代码上方显得很厚，
	// 用户反映「上下高度有些大，影响审阅代码」。现在把刻度数字与列号色块并到
	// 同一行、色带压到底部，总高度降到 20px —— 比一行代码的行距还矮。
	rulerHeight = 20
	// rulerTickStep 是带数字刻度的间隔（每 10 列一个）。
	rulerTickStep = 10
	// maxRulerTicks 是刻度对象池大小；超出视口宽度的刻度不再绘制。
	maxRulerTicks = 96

	// 纵向分区（像素）。20px 高从上到下依次是：
	//   [0, rulerLabelH)              刻度数字 + 列号色块
	//   [rulerTickY, +rulerTickH)     刻度线
	//   [rulerBandY, rulerHeight)     色带（压在底部分隔线上）
	rulerLabelH = 12
	rulerTickY  = 12
	rulerTickH  = 3
	rulerBandY  = 15
	rulerBandH  = 5

	// rulerTextSize 是刻度数字与列号文字的磅值（比 CaptionText 略小一点，
	// 才能在 12px 的行里排下数字而不把标尺顶高）。
	rulerTextSize = 10
	// rulerChipPad 是列号色块左右的内边距。
	rulerChipPad = 4
)

func newColumnRuler() *ColumnRuler {
	r := &ColumnRuler{}
	r.ExtendBaseWidget(r)
	return r
}

// Set 更新标尺的参考行。
func (r *ColumnRuler) Set(lineNo int, text string, spans []Span, shift int) {
	r.lineNo, r.text, r.spans, r.shift = lineNo, text, spans, shift
	r.Refresh()
}

// Line 返回参考行行号（1-based，0 表示无）。
func (r *ColumnRuler) Line() int { return r.lineNo }

// Spans 返回参考行上的列范围（拷贝）。
func (r *ColumnRuler) Spans() []Span {
	out := make([]Span, len(r.spans))
	copy(out, r.spans)
	return out
}

// MinSize 返回标尺的固定高度。
func (r *ColumnRuler) MinSize() fyne.Size { return fyne.NewSize(240, rulerHeight) }

func (r *ColumnRuler) CreateRenderer() fyne.WidgetRenderer {
	rr := &columnRulerRenderer{ruler: r}
	rr.bg = canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	rr.border = canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	rr.gutter = canvas.NewText("列", colorGutterText)
	rr.gutter.Alignment = fyne.TextAlignTrailing
	rr.objects = append(rr.objects, rr.bg, rr.border, rr.gutter)

	for i := 0; i < maxRulerTicks; i++ {
		tick := canvas.NewRectangle(colorGutterText)
		rr.ticks = append(rr.ticks, tick)
		rr.objects = append(rr.objects, tick)

		lbl := canvas.NewText("", colorGutterText)
		rr.tickLabels = append(rr.tickLabels, lbl)
		rr.objects = append(rr.objects, lbl)
	}
	for i := 0; i < maxSpansPerLine; i++ {
		band := canvas.NewRectangle(color.Transparent)
		band.CornerRadius = 2
		rr.bands = append(rr.bands, band)
		rr.objects = append(rr.objects, band)
	}
	// 指示竖线 + 列号色块：色块必须排在刻度数字之后，才能盖住底下的数字，
	// 避免「列 26-28」和「40」这种刻度数字糊成一团。
	rr.caret = canvas.NewRectangle(color.Transparent)
	rr.chipBG = canvas.NewRectangle(color.Transparent)
	rr.chipBG.CornerRadius = 3
	rr.chip = canvas.NewText("", colorCodeText)
	rr.objects = append(rr.objects, rr.caret, rr.chipBG, rr.chip)
	return rr
}

type columnRulerRenderer struct {
	ruler   *ColumnRuler
	objects []fyne.CanvasObject

	bg     *canvas.Rectangle
	border *canvas.Rectangle
	gutter *canvas.Text

	ticks      []*canvas.Rectangle
	tickLabels []*canvas.Text
	bands      []*canvas.Rectangle

	caret  *canvas.Rectangle
	chipBG *canvas.Rectangle
	chip   *canvas.Text
}

// spanTextColor 返回定位标识文字的颜色（比色带更深，保证可读）。
func spanTextColor(k HLKind) color.Color {
	switch k {
	case HLSink:
		return color.NRGBA{R: 0xB0, G: 0x1B, B: 0x1B, A: 0xFF}
	case HLSource:
		return color.NRGBA{R: 0xA1, G: 0x5C, B: 0x00, A: 0xFF}
	}
	return colorCodeText
}

func (rr *columnRulerRenderer) Layout(size fyne.Size) {
	r := rr.ruler
	rr.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	rr.bg.Move(fyne.NewPos(0, 0))
	rr.bg.Resize(size)
	rr.border.FillColor = theme.Color(theme.ColorNameSeparator)
	rr.border.Move(fyne.NewPos(0, size.Height-1))
	rr.border.Resize(fyne.NewSize(size.Width, 1))

	for _, t := range rr.ticks {
		t.Hide()
	}
	for _, l := range rr.tickLabels {
		l.Hide()
	}
	for _, b := range rr.bands {
		b.Hide()
	}
	rr.caret.Hide()
	rr.chipBG.Hide()
	rr.chip.Hide()

	rr.gutter.TextSize = rulerTextSize
	rr.gutter.Move(fyne.NewPos(0, 0))
	rr.gutter.Resize(fyne.NewSize(gutterWidth-8, rulerLabelH))

	if r.text == "" || size.Width <= gutterWidth {
		return
	}
	// 列像素必须与 codeLine 用同一套字体度量（含 codeTextOriginX 的
	// innerPadding 偏移，也就是 RichText 真正画字形的起点），否则标尺会和代码错位。
	measure := func(s string) float32 {
		return fyne.MeasureText(s, codeTextSize(), codeStyle()).Width
	}
	xAt := func(col int) float32 {
		if col <= 1 {
			return codeTextOriginX()
		}
		return codeTextOriginX() + measure(sliceRunes(r.text, 0, col-1))
	}
	origCol := func(w int) int {
		if r.shift > 0 {
			return w + r.shift - 1
		}
		return w
	}

	// ---------- 刻度：每 10 列一根刻度线 + 列号数字 ----------
	ti := 0
	maxCol := len([]rune(r.text)) + 1
	for w := 1; w <= maxCol && ti < len(rr.ticks); w++ {
		if r.shift > 0 && w == 1 {
			continue // 「…」是裁剪占位，不是真实列
		}
		orig := origCol(w)
		if orig <= 0 || orig%rulerTickStep != 0 {
			continue
		}
		x := xAt(w)
		if x > size.Width-6 {
			break
		}
		tick := rr.ticks[ti]
		tick.FillColor = theme.Color(theme.ColorNameSeparator)
		tick.Move(fyne.NewPos(x, rulerTickY))
		tick.Resize(fyne.NewSize(1, rulerTickH))
		tick.Show()

		lbl := rr.tickLabels[ti]
		lbl.Text = strconv.Itoa(orig)
		lbl.Color = colorGutterText
		lbl.TextSize = rulerTextSize
		lbl.Move(fyne.NewPos(x+2, 0))
		lbl.Resize(fyne.NewSize(60, rulerLabelH))
		lbl.Show()
		ti++
	}

	// ---------- 色带 + 列号色块 ----------
	first := true
	for i, sp := range r.spans {
		if i >= len(rr.bands) {
			break
		}
		start := sp.StartCol
		if start < 1 {
			start = 1
		}
		end := sp.EndCol
		if end > maxCol {
			end = maxCol
		}
		if end <= start {
			end = start + 1
		}
		x1, x2 := xAt(start), xAt(end)
		if x1 > size.Width {
			continue
		}
		if x2 > size.Width {
			x2 = size.Width
		}
		if w := x2 - x1; w < 3 {
			x2 = x1 + 3
		}
		band := rr.bands[i]
		band.FillColor = spanColor(sp.Kind)
		band.Move(fyne.NewPos(x1, rulerBandY))
		band.Resize(fyne.NewSize(x2-x1, rulerBandH))
		band.Show()

		if !first {
			continue // 只给第一条（主定位）贴列号，避免标尺上堆满文字
		}
		first = false
		colText := columnRangeText(sp)
		if colText == "" {
			continue
		}
		text := "列 " + colText
		color := spanTextColor(sp.Kind)
		chipW := fyne.MeasureText(text, rulerTextSize, codeStyle()).Width + 2*rulerChipPad

		// 色块居中压在色带正上方，再用一根竖线连到色带中心：
		// 这样「列 26-28」和它标注的那一段高亮是同一个视觉整体，
		// 不会被误读成旁边刻度数字的注释。靠边时夹在可视区内。
		bandCenter := (x1 + x2) / 2
		cx := clampPos(bandCenter-chipW/2, gutterWidth, size.Width-chipW)

		caretX := clampPos(bandCenter, cx, cx+chipW-2)
		rr.caret.FillColor = color
		rr.caret.Move(fyne.NewPos(caretX, rulerLabelH))
		rr.caret.Resize(fyne.NewSize(2, rulerBandY-rulerLabelH))
		rr.caret.Show()

		rr.chipBG.FillColor = spanColor(sp.Kind)
		rr.chipBG.Move(fyne.NewPos(cx, 0))
		rr.chipBG.Resize(fyne.NewSize(chipW, rulerLabelH))
		rr.chipBG.Show()

		rr.chip.Text = text
		rr.chip.Color = color
		rr.chip.TextSize = rulerTextSize
		rr.chip.Move(fyne.NewPos(cx+rulerChipPad, 0))
		rr.chip.Resize(fyne.NewSize(chipW-2*rulerChipPad, rulerLabelH))
		rr.chip.Show()
	}
}

// columnRangeText 返回与证据链完全一致的列号文案。
//
// 取值一律用 CSV 原值（Span.OrigStart/OrigEnd），规则与 model.Location.ColumnRange 相同：
//   - 单点位置（start == end）：只显示起始列；
//   - 多列：显示「起始列-结束列」；
//   - 跨行高亮在首/中间行一直延续到行尾：显示「起始列-行尾」。
func columnRangeText(sp Span) string {
	start := sp.OrigStart
	if start <= 0 {
		start = sp.StartCol
	}
	if start <= 0 {
		return ""
	}
	end := sp.OrigEnd
	switch {
	case end == 0 || end == start:
		return strconv.Itoa(start)
	case end >= spanToLineEnd:
		return strconv.Itoa(start) + "-行尾"
	default:
		return strconv.Itoa(start) + "-" + strconv.Itoa(end)
	}
}

func clampPos(v, lo, hi float32) float32 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (rr *columnRulerRenderer) MinSize() fyne.Size { return rr.ruler.MinSize() }

func (rr *columnRulerRenderer) Refresh() {
	rr.Layout(rr.ruler.Size())
	canvas.Refresh(rr.ruler)
}

func (rr *columnRulerRenderer) Objects() []fyne.CanvasObject { return rr.objects }

func (rr *columnRulerRenderer) Destroy() {}
