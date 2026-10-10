package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// 本文件解决「整个界面的灰字看得费眼」。
//
// Fyne 内置浅色主题里，次要文字用的是 ColorNameDisabled = #E3E3E3：
// 纯白背景上的对比度只有约 1.3:1，而界面里的文件名、行号、统计、译文来源、
// 操作提示、英文原文全部是 LowImportance（= ColorNameDisabled），
// 结果就是「一眼看过去全是浅灰，得凑近看」。
// 深色主题那边的 ColorNameDisabled = #39393A 同样几乎看不见。
//
// 这里只覆盖「读文字必须用到的几个颜色」，其余交给系统主题：
//   - 前景色压深（浅色 #1F2937 / 深色 #E9EEF6），正文对比度 ≈ 14:1；
//   - 次要文字（LowImportance / 禁用文字）改为可读的中灰，
//     浅色 #5B6472（对比度 ≈ 5.9:1）、深色 #AEB9C9（≈ 7.6:1），
//     既看得清，又明显比正文浅，主次层次还在；
//   - 输入框占位符、分隔线、控件底色一起对齐，避免出现「只有正文变黑、
//     其它还是灰蒙蒙」的割裂感。
var (
	// 浅色主题
	lightForeground   = color.NRGBA{R: 0x1F, G: 0x29, B: 0x37, A: 0xFF}
	lightSecondary    = color.NRGBA{R: 0x5B, G: 0x64, B: 0x72, A: 0xFF}
	lightPlaceholder  = color.NRGBA{R: 0x6B, G: 0x72, B: 0x80, A: 0xFF}
	lightSeparator    = color.NRGBA{R: 0xD4, G: 0xDA, B: 0xE2, A: 0xFF}
	lightInputBorder  = color.NRGBA{R: 0xC3, G: 0xCB, B: 0xD6, A: 0xFF}
	lightInputBG      = color.NRGBA{R: 0xF4, G: 0xF6, B: 0xF9, A: 0xFF}
	lightButton       = color.NRGBA{R: 0xF1, G: 0xF4, B: 0xF8, A: 0xFF}
	lightHeaderBG     = color.NRGBA{R: 0xF7, G: 0xF9, B: 0xFC, A: 0xFF}
	lightMenuBG       = color.NRGBA{R: 0xF7, G: 0xF9, B: 0xFC, A: 0xFF}
	lightDisabledBtn  = color.NRGBA{R: 0xEE, G: 0xF1, B: 0xF5, A: 0xFF}
	lightScrollbarBG  = color.NRGBA{R: 0xE2, G: 0xE6, B: 0xEC, A: 0xFF}
	lightSeparatorAlt = color.NRGBA{R: 0xD4, G: 0xDA, B: 0xE2, A: 0xFF}

	// 深色主题
	darkForeground  = color.NRGBA{R: 0xE9, G: 0xEE, B: 0xF6, A: 0xFF}
	darkSecondary   = color.NRGBA{R: 0xAE, G: 0xB9, B: 0xC9, A: 0xFF}
	darkPlaceholder = color.NRGBA{R: 0x9A, G: 0xA5, B: 0xB5, A: 0xFF}
	darkSeparator   = color.NRGBA{R: 0x3A, G: 0x42, B: 0x4E, A: 0xFF}
	darkInputBorder = color.NRGBA{R: 0x46, G: 0x50, B: 0x5E, A: 0xFF}
	darkDisabledBtn = color.NRGBA{R: 0x2E, G: 0x33, B: 0x3B, A: 0xFF}
)

// readableColor 返回覆盖后的高对比度颜色；未覆盖的名字返回 ok=false，交给底层主题。
//
// 参数 v 由 Fyne 传入，在 macOS / Windows 上已经被解析成明确的浅色或深色
// （见 app.DefaultVariant），因此这里可以直接据此分支。
func readableColor(n fyne.ThemeColorName, v fyne.ThemeVariant) (color.Color, bool) {
	dark := v == theme.VariantDark
	switch n {
	case theme.ColorNameForeground:
		if dark {
			return darkForeground, true
		}
		return lightForeground, true
	// LowImportance（次要文字）与「禁用控件文字」在 Fyne 里是同一个颜色名。
	// 界面里大量提示文字用的都是 LowImportance，所以这里必须给一个能看清的值。
	case theme.ColorNameDisabled:
		if dark {
			return darkSecondary, true
		}
		return lightSecondary, true
	case theme.ColorNamePlaceHolder:
		if dark {
			return darkPlaceholder, true
		}
		return lightPlaceholder, true
	case theme.ColorNameSeparator:
		if dark {
			return darkSeparator, true
		}
		return lightSeparator, true
	case theme.ColorNameInputBorder:
		if dark {
			return darkInputBorder, true
		}
		return lightInputBorder, true
	case theme.ColorNameDisabledButton:
		if dark {
			return darkDisabledBtn, true
		}
		return lightDisabledBtn, true
	}
	if !dark {
		switch n {
		case theme.ColorNameInputBackground:
			return lightInputBG, true
		case theme.ColorNameButton:
			return lightButton, true
		case theme.ColorNameHeaderBackground:
			return lightHeaderBG, true
		case theme.ColorNameMenuBackground:
			return lightMenuBG, true
		case theme.ColorNameScrollBarBackground:
			return lightScrollbarBG, true
		case theme.ColorNameInnerWindowBorder:
			return lightSeparatorAlt, true
		}
	}
	return nil, false
}
