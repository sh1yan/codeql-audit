package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// 各栏允许被压缩到的最小宽度（像素）。
//
// 分隔条能拖到哪，完全由它们决定：Fyne 的 container.Split 把分隔条位置夹在
// [前侧最小宽, 可用宽 - 后侧最小宽] 之间；而 Scroll（VerticalOnly）的最小宽度
// 又直接取自内容的最小宽度。于是「内容里有一行很长的、不支持换行的东西」=
// 「这一栏永远没法收窄」。
const (
	// detailMinWidth 是右栏（漏洞详情）的最小宽度。
	//
	// 取 260：再窄的话标题与正文每行只能放下十几个汉字，读起来反而更累。
	// 中栏（代码视图）的最小宽度保持它自己的天然值（约 300，由顶部两个按钮决定），
	// 所以右栏最大可以拖到「窗口宽 - 中栏最小宽」，足够宽了。
	detailMinWidth float32 = 260
)

// clampWidthLayout 是一个只包裹单个子元素的布局：
// 子元素始终铺满整个可用区域，但对外只声明 minWidth 作为最小宽度。
//
// 为什么需要它（回归「右栏怎么拖都拖不窄」）：
// 详情面板里有一串「证据链」按钮，Fyne 的 Button 不支持换行，它的 MinSize.Width
// 就是整行文字的宽度；实测在 1512 宽的窗口里，这些按钮把右栏的最小宽度撑到了
// 600~830px，于是 Split 把分隔条死死夹住——用户既不能把右栏拖窄，也没法把左栏
// 拖宽（外层分隔条的后侧最小宽同样被撑大了）。
//
// 包一层这个布局后，最小宽度由调用方说了算，分隔条就能自由拖动；
// 内容仍然按真实宽度铺满，所以文字换行、滚动都照常工作。
//
// 注意：MinSize.Height 仍然取子元素的最小高度，避免把面板压成 0 高。
type clampWidthLayout struct {
	minWidth float32
}

func (l clampWidthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(l.minWidth, 0)
	for _, o := range objs {
		if o == nil || !o.Visible() {
			continue
		}
		if h := o.MinSize().Height; h > min.Height {
			min.Height = h
		}
	}
	return min
}

func (l clampWidthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if size.Width < 0 {
		size.Width = 0
	}
	if size.Height < 0 {
		size.Height = 0
	}
	for _, o := range objs {
		if o == nil {
			continue
		}
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// clampWidth 把内容包成「最小宽度可控」的面板。
func clampWidth(content fyne.CanvasObject, minWidth float32) *fyne.Container {
	return container.New(clampWidthLayout{minWidth: minWidth}, content)
}
