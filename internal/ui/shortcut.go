package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"codeql-audit/internal/config"
)

// 本文件把「可自定义的键盘快捷键」接到界面上。
//
// 目前只有一个可配置快捷键：在当前代码文件中查找（默认 Cmd/Ctrl+F）。
// 用户在「设置 → 代码视图 → 查找快捷键」里改完立即生效，并写进配置
// （code_search_shortcut），下次启动照旧。

// fyneShortcut 把配置层的快捷键描述翻译成 Fyne 的快捷键对象。
//
// Ctrl 与 Cmd 分别映射到 Fyne 的 Control 与 Super 修饰键：
// macOS 上 Control 就是键盘上的 Control，Command 是 Super，
// 其它平台上 Super 对应 Windows 键，语义与配置层一致。
func fyneShortcut(spec config.ShortcutSpec) fyne.Shortcut {
	var mod fyne.KeyModifier
	if spec.Ctrl {
		mod |= fyne.KeyModifierControl
	}
	if spec.Cmd {
		mod |= fyne.KeyModifierSuper
	}
	if spec.Alt {
		mod |= fyne.KeyModifierAlt
	}
	if spec.Shift {
		mod |= fyne.KeyModifierShift
	}
	return &desktop.CustomShortcut{KeyName: fyne.KeyName(spec.Key), Modifier: mod}
}

// shortcutCanvas 是注册快捷键所需的最小画布能力。
//
// 单独抽成接口是为了让测试能塞一个「记录调用」的假画布进来断言
// 「新键位注册了、旧键位注销了」——Fyne 的测试画布把快捷键处理器包在
// 内部接口里，测试代码既拿不到也派发不了（见 codesearch_test.go）。
type shortcutCanvas interface {
	AddShortcut(shortcut fyne.Shortcut, handler func(shortcut fyne.Shortcut))
	RemoveShortcut(shortcut fyne.Shortcut)
}

// shortcutTarget 返回注册快捷键的目标画布（测试里可以是假画布）。
func (a *App) shortcutTarget() shortcutCanvas {
	if a.shortcutCanvas != nil {
		return a.shortcutCanvas
	}
	if a.win == nil {
		return nil
	}
	return a.win.Canvas()
}

// applySearchShortcut 注册（或重新注册）「在当前代码文件中查找」的快捷键。
//
// 这里同时挂两处：
//
//  1. 画布级快捷键（canvas.AddShortcut）——最直接的注册方式；
//  2. 「编辑」主菜单里的同名菜单项（带同一个 Shortcut）。
//
// 为什么必须两个都挂：Fyne 的按键分发顺序是「主菜单快捷键 → 当前聚焦对象 →
// 画布快捷键」（见 internal/driver/glfw/window.go）。代码区的正文是可选中的
// Label，它实现了 fyne.Shortcutable，用户只要点过代码、焦点落在它身上，
// Cmd/Ctrl+F 就会被它吃掉，画布级快捷键根本收不到。主菜单快捷键在聚焦对象
// 之前处理，因此「点过代码之后按 Cmd+F」这条路才是可靠的。
//
// 顺带的好处：菜单项上会显示键位（macOS 还会显示 ⌘/⌃ 符号），用户能直接看到
// 当前生效的是哪个键，而不用去翻设置。
func (a *App) applySearchShortcut() {
	spec, err := config.ParseShortcut(a.cfg.SearchShortcut())
	if err != nil {
		spec, _ = config.ParseShortcut(config.DefaultSearchShortcut())
	}
	sc := fyneShortcut(spec)

	if canvas := a.shortcutTarget(); canvas != nil {
		if a.searchShortcut != nil {
			canvas.RemoveShortcut(a.searchShortcut)
		}
		a.searchShortcut = sc
		canvas.AddShortcut(sc, func(fyne.Shortcut) { a.openCodeSearch() })
	}

	if a.win != nil {
		item := fyne.NewMenuItem("在当前文件中查找", a.openCodeSearch)
		item.Shortcut = sc
		a.searchMenuItem = item
		a.win.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("编辑", item)))
	}

	// 查找框的提示里也写上当前键位，用户一眼能看出改没改成功。
	if a.codeView != nil {
		a.codeView.SetSearchHint(spec.String())
	}
}

// openCodeSearch 打开代码视图的查找工具条。
//
// 三个入口共用：快捷键、顶栏「查找代码」按钮、「编辑」菜单项。
func (a *App) openCodeSearch() {
	if a.codeView == nil {
		return
	}
	if a.codeView.Path() == "" {
		a.statsLbl.SetText("查找：当前还没有打开源码文件，先在左侧选一条漏洞。")
		return
	}
	a.codeView.OpenSearch()
}
