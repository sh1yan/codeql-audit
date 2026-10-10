package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// 证据链行的内边距与图标尺寸（像素）。
const (
	chainRowPad      float32 = 8
	chainRowIconSize float32 = 18
)

// chainRow 是证据链里的一行：整行可点击，并且**文字会按面板宽度换行**。
//
// 为什么不用 widget.Button：
// Fyne 的 Button 渲染器既不支持换行，也不加省略号（Button 上根本没有
// Wrapping 字段）。它的 MinSize.Width 就是整行文字的宽度，行高固定一行；
// 面板一窄，后半截「第 N 行 · 标签 · → 跳转」就会被直接裁掉 —— 这正是
// 「漏洞详情 → 证据链的内容不会换行」的原因。
//
// 这里改成自绘 Widget：圆角背景 + TextWrapWord 的 Label + 左侧图标，
// 行高随折行后的文字自动增长，整行任意位置都能触发跳转。
type chainRow struct {
	widget.BaseWidget

	text  string
	icon  fyne.Resource
	onTap func()

	hovered bool
}

// newChainRow 创建一行证据链。
func newChainRow(text string, icon fyne.Resource, onTap func()) *chainRow {
	r := &chainRow{text: text, icon: icon, onTap: onTap}
	r.ExtendBaseWidget(r)
	return r
}

// Tapped 让整行（含折行之后的每一行文字）都能触发跳转。
func (r *chainRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

// MouseIn / MouseOut 只做一个悬停高亮，让用户知道整行可点。
func (r *chainRow) MouseIn(*fyne.PointEvent) {
	if !r.hovered {
		r.hovered = true
		r.Refresh()
	}
}

// MouseOut 取消悬停高亮。
func (r *chainRow) MouseOut() {
	if r.hovered {
		r.hovered = false
		r.Refresh()
	}
}

func (r *chainRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 6
	rr := &chainRowRenderer{
		row:   r,
		bg:    bg,
		icon:  widget.NewIcon(r.icon),
		label: wrapLabel(r.text),
	}
	rr.objects = []fyne.CanvasObject{bg, rr.icon, rr.label}
	return rr
}

type chainRowRenderer struct {
	row   *chainRow
	bg    *canvas.Rectangle
	icon  *widget.Icon
	label *widget.Label

	// measured 记录上次按哪个宽度给 Label 做过折行，避免 MinSize 里反复重排。
	measured float32
	objects  []fyne.CanvasObject
}

// textWidth 返回这一行留给文字的实际宽度。
func (rr *chainRowRenderer) textWidth(rowWidth float32) float32 {
	w := rowWidth - chainRowPad*3 - chainRowIconSize
	if w < 1 {
		w = 1
	}
	return w
}

// wrapTo 先按给定行宽让 Label 重新折行，再返回它需要的高度。
//
// Fyne 的 Label（内部是 RichText）是先拿到宽度、再算折行的，所以必须先
// Resize 到目标宽度，它的 MinSize 才会给出折行后的真实高度。
func (rr *chainRowRenderer) wrapTo(rowWidth float32) {
	if rowWidth <= 0 {
		// 还没被父容器布局过：用详情面板的最小宽度保守估算，
		// 宁可多算几行也不能少算（少算会把最后一行的文字裁掉）。
		rowWidth = detailMinWidth
	}
	w := rr.textWidth(rowWidth)
	if w == rr.measured {
		return
	}
	rr.measured = w
	rr.label.Resize(fyne.NewSize(w, rr.label.MinSize().Height))
}

func (rr *chainRowRenderer) minHeight() float32 {
	h := rr.label.MinSize().Height + chainRowPad*2
	if h < chainRowIconSize+chainRowPad {
		h = chainRowIconSize + chainRowPad
	}
	return h
}

func (rr *chainRowRenderer) Layout(size fyne.Size) {
	rr.wrapTo(size.Width)
	h := rr.minHeight()
	rr.bg.Move(fyne.NewPos(0, 0))
	rr.bg.Resize(fyne.NewSize(size.Width, h))
	// 悬停时描一圈主色边，让用户知道整行可点（同时保持和按钮一致的底色）。
	rr.bg.FillColor = theme.Color(theme.ColorNameButton)
	if rr.row.hovered {
		rr.bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
		rr.bg.StrokeWidth = 1.5
	} else {
		rr.bg.StrokeWidth = 0
	}
	rr.icon.Move(fyne.NewPos(chainRowPad, (h-chainRowIconSize)/2))
	rr.icon.Resize(fyne.NewSize(chainRowIconSize, chainRowIconSize))
	rr.label.Resize(fyne.NewSize(rr.textWidth(size.Width), rr.label.MinSize().Height))
	rr.label.Move(fyne.NewPos(chainRowPad*2+chainRowIconSize, chainRowPad))
}

// MinSize 只声明一个很小的宽度（折行之后文字宽度由面板决定），
// 高度取折行后的真实高度，这样 VBox 才能把整行完整显示出来。
func (rr *chainRowRenderer) MinSize() fyne.Size {
	rr.wrapTo(rr.row.Size().Width)
	w := rr.label.MinSize().Width
	if w > 0 {
		w += chainRowPad*3 + chainRowIconSize
	}
	return fyne.NewSize(w, rr.minHeight())
}

func (rr *chainRowRenderer) Refresh() {
	// 文本或主题变化后需要重新折行。
	rr.measured = 0
	rr.wrapTo(rr.row.Size().Width)
	canvas.Refresh(rr.row)
}

func (rr *chainRowRenderer) Objects() []fyne.CanvasObject { return rr.objects }

func (rr *chainRowRenderer) Destroy() {}
