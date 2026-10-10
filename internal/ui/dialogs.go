package ui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/config"
	"codeql-audit/internal/model"
	"codeql-audit/internal/translate"
)

// showFileDialog 显示文件/目录选择框并按需调整尺寸。
//
// 注意：必须先 Show 再 Resize。Fyne v2.8.1 的 FileDialog.Resize 会先调用
// MinSize()，而 MinSize() 直接解引用 Show 之前尚未创建的 f.dialog，
// 于是「先 Resize 再 Show」必定空指针崩溃（表现为点击按钮后程序闪退）。
// 新建 dialog.NewFileOpen / NewFileSave / NewFolderOpen 后一律走本函数。
func showFileDialog(d *dialog.FileDialog, size fyne.Size) {
	d.Show()
	if !size.IsZero() {
		d.Resize(size)
	}
}

// showSettings 显示大模型与翻译设置。
func (a *App) showSettings() {
	s := a.cfg.LLM
	// 注意：这是**翻译**的总开关，只影响「未命中翻译库时调用大模型」。
	// AI 误报验证有自己的开关（见下面的 aiEnabled），二者互不影响。
	enabled := widget.NewCheck("启用大模型翻译兜底（仅在未命中翻译库时调用）", nil)
	enabled.SetChecked(s.Enabled)
	base := widget.NewEntry()
	base.SetText(s.BaseURL)
	base.SetPlaceHolder("例如 https://api.deepseek.com/v1 或 http://localhost:11434/v1")
	key := widget.NewPasswordEntry()
	key.SetText(s.APIKey)
	modelName := widget.NewEntry()
	modelName.SetText(s.Model)
	modelName.SetPlaceHolder("例如 deepseek-chat / gpt-4o-mini / qwen2.5:7b")
	timeout := widget.NewEntry()
	t := s.TimeoutSeconds
	if t <= 0 {
		t = 60
	}
	timeout.SetText(strconv.Itoa(t))
	templateOnly := widget.NewCheck("只发送消息模板与占位符（不发送源码内容）", nil)
	templateOnly.SetChecked(s.TemplateOnly)
	dbPath := widget.NewEntry()
	dbPath.SetText(a.cfg.TranslateDB)
	dbPath.Disable()

	fontEntry := widget.NewEntry()
	fontEntry.SetText(a.cfg.FontPath)
	fontEntry.SetPlaceHolder("留空自动探测；也可填写 .ttf/.otf/.ttc 中文字体路径")
	fontInfo := widget.NewLabel(a.fontInfo.Report())
	fontInfo.Wrapping = fyne.TextWrapWord
	pickFontBtn := widget.NewButton("选择字体文件…", func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			fontEntry.SetText(rc.URI().Path())
			_ = rc.Close()
		}, a.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".ttf", ".otf", ".ttc", ".otc"}))
		showFileDialog(fd, fyne.NewSize(900, 600))
	})

	// ---------- 用编辑器打开 ----------
	editorCmd := widget.NewEntry()
	editorCmd.SetText(a.cfg.Editor.Cmd)
	editorCmd.SetPlaceHolder("编辑器可执行文件，例如 /usr/local/bin/code 或 /Applications/Visual Studio Code.app")
	editorArgs := widget.NewEntry()
	editorArgs.SetText(a.cfg.Editor.Args)
	editorArgs.SetPlaceHolder("参数模板，默认 " + defaultEditorArgs)
	pickEditorBtn := widget.NewButton("选择编辑器…", func() {
		fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			editorCmd.SetText(rc.URI().Path())
			_ = rc.Close()
		}, a.win)
		showFileDialog(fd, fyne.NewSize(900, 600))
	})
	editorTip := widget.NewLabel("参数模板支持的占位符：{file} 文件绝对路径、{line} 行号、{col} 列号。" +
		"默认 " + defaultEditorArgs + " 适用于 VS Code / Cursor；Sublime 可用 \"{file}:{line}\"。" +
		"也可以直接填 macOS 的 .app 包路径（工具会自动走 open -a）。")
	editorTip.Wrapping = fyne.TextWrapWord
	editorTip.Importance = widget.LowImportance
	testEditorBtn := widget.NewButton("测试打开当前文件", func() {
		saved := a.cfg.Editor
		a.cfg.Editor = config.EditorSettings{
			Cmd:  strings.TrimSpace(editorCmd.Text),
			Args: strings.TrimSpace(editorArgs.Text),
		}
		if a.codeView.Path() == "" {
			dialog.ShowInformation("提示", "还没有打开任何文件，请先在左侧选择一条漏洞再测试。", a.win)
			a.cfg.Editor = saved
			return
		}
		line := a.curLine
		if line <= 0 {
			line = 1
		}
		cmd, desc, err := editorCommand(a.cfg.Editor, a.codeView.Path(), line, 1)
		if err == nil {
			err = cmd.Start()
		}
		a.cfg.Editor = saved
		if err != nil {
			dialog.ShowError(fmt.Errorf("启动失败：%w", err), a.win)
			return
		}
		a.statsLbl.SetText("测试成功，已执行：" + desc)
	})

	sendCode := widget.NewCheck("AI 验证时把沉淀点/来源点的源码片段发给大模型", nil)
	sendCode.SetChecked(a.cfg.AISendCode())
	verifyTip := widget.NewLabel("关闭后 AI 验证只发送规则、漏洞详情与文件位置，不发送任何源码内容。" +
		"翻译功能另有独立的「隐私」开关，二者互不影响。")
	verifyTip.Wrapping = fyne.TextWrapWord
	verifyTip.Importance = widget.LowImportance

	// ---------- 代码视图 ----------
	showRuler := widget.NewCheck("显示列标尺（在中栏代码上方标出「第几列」）", nil)
	showRuler.SetChecked(a.cfg.ColumnRuler())
	rulerTip := widget.NewLabel("列标尺与代码列位置严格对齐：每 10 列一根刻度线并标出列号，" +
		"当前聚焦的沉淀点/来源点位置会画成色带，并贴着「列 26-28」色块（列号取自 CSV 原值）。" +
		"整条标尺只有 20px 高，关闭后这份高度会立刻还给代码。")
	rulerTip.Wrapping = fyne.TextWrapWord
	rulerTip.Importance = widget.LowImportance

	// 查找快捷键：中栏代码区按这个键会弹出查找条，回车在命中之间跳转。
	searchKey := widget.NewEntry()
	searchKey.SetText(a.cfg.SearchShortcut())
	searchKey.SetPlaceHolder("默认 " + config.DefaultSearchShortcut())
	searchKey.Validator = func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil // 留空表示恢复默认
		}
		_, err := config.ParseShortcut(s)
		return err
	}
	resetSearchKeyBtn := widget.NewButton("恢复默认", func() {
		searchKey.SetText(config.DefaultSearchShortcut())
	})
	searchTip := widget.NewLabel("格式：修饰键 + 主键，例如 Cmd+F（macOS 的 Command）、Ctrl+F、" +
		"Ctrl+Shift+G、Alt+F3。可用修饰键：Cmd / Ctrl / Alt / Shift；主键支持字母 A-Z、数字 0-9、" +
		"F1-F12 与常见符号键（/ \\ - = . , ; [ ]）。留空即恢复默认。")
	searchTip.Wrapping = fyne.TextWrapWord
	searchTip.Importance = widget.LowImportance

	// AI 误报验证有自己独立的总开关：以前它错误地复用上面的翻译开关，
	// 用户填对了接口地址/模型/Key，只要没勾翻译开关就永远无法验证。
	aiEnabled := widget.NewCheck("启用 AI 误报验证（与上面的翻译开关相互独立）", nil)
	aiEnabled.SetChecked(a.cfg.AIVerify())
	aiTip := widget.NewLabel("勾选后，右栏漏洞详情里的「AI 验证本条」与顶栏「批量 AI 验证」才可用。" +
		"上面的「大模型翻译兜底」只影响翻译，关掉它不会影响 AI 误报验证。")
	aiTip.Wrapping = fyne.TextWrapWord
	aiTip.Importance = widget.LowImportance
	testConnBtn := widget.NewButton("测试连接", func() {
		to, _ := strconv.Atoi(strings.TrimSpace(timeout.Text))
		if to <= 0 {
			to = 60
		}
		a.testLLMConnection(config.LLMSettings{
			BaseURL:        strings.TrimSpace(base.Text),
			APIKey:         strings.TrimSpace(key.Text),
			Model:          strings.TrimSpace(modelName.Text),
			TimeoutSeconds: to,
		})
	})

	items := []*widget.FormItem{
		widget.NewFormItem("", sectionLabel("大模型（OpenAI 兼容接口）")),
		widget.NewFormItem("", enabled),
		widget.NewFormItem("接口地址", base),
		widget.NewFormItem("API Key", key),
		widget.NewFormItem("模型", modelName),
		widget.NewFormItem("超时（秒）", timeout),
		widget.NewFormItem("翻译隐私", templateOnly),
		widget.NewFormItem("翻译库路径", dbPath),
		widget.NewFormItem("", widget.NewSeparator()),
		widget.NewFormItem("", sectionLabel("AI 误报验证")),
		widget.NewFormItem("", aiEnabled),
		widget.NewFormItem("", aiTip),
		widget.NewFormItem("验证隐私", sendCode),
		widget.NewFormItem("", verifyTip),
		widget.NewFormItem("", testConnBtn),
		widget.NewFormItem("", widget.NewSeparator()),
		widget.NewFormItem("", sectionLabel("界面字体")),
		widget.NewFormItem("界面字体", container.NewBorder(nil, nil, nil, pickFontBtn, fontEntry)),
		widget.NewFormItem("", fontInfo),
		widget.NewFormItem("", widget.NewSeparator()),
		widget.NewFormItem("", sectionLabel("代码视图")),
		widget.NewFormItem("", showRuler),
		widget.NewFormItem("", rulerTip),
		widget.NewFormItem("查找快捷键",
			container.NewBorder(nil, nil, nil, resetSearchKeyBtn, searchKey)),
		widget.NewFormItem("", searchTip),
		widget.NewFormItem("", widget.NewSeparator()),
		widget.NewFormItem("", sectionLabel("用编辑器打开")),
		widget.NewFormItem("编辑器", container.NewBorder(nil, nil, nil, pickEditorBtn, editorCmd)),
		widget.NewFormItem("参数模板", editorArgs),
		widget.NewFormItem("", editorTip),
		widget.NewFormItem("", testEditorBtn),
	}
	// 用可滚动的自定义对话框而不是 dialog.NewForm：
	// 分组之后表单内容接近 1100px 高，固定高度的 NewForm 会把底部
	// 「用编辑器打开」整段裁掉且无法滚动。
	form := widget.NewForm(items...)
	scroll := container.NewVScroll(form)
	scroll.SetMinSize(fyne.NewSize(760, 620))
	d := dialog.NewCustomConfirm("设置", "保存", "取消", scroll, func(ok bool) {
		if !ok {
			return
		}
		to, _ := strconv.Atoi(strings.TrimSpace(timeout.Text))
		if to <= 0 {
			to = 60
		}
		a.cfg.LLM = config.LLMSettings{
			Enabled:        enabled.Checked,
			BaseURL:        strings.TrimSpace(base.Text),
			APIKey:         strings.TrimSpace(key.Text),
			Model:          strings.TrimSpace(modelName.Text),
			TimeoutSeconds: to,
			TemplateOnly:   templateOnly.Checked,
		}
		a.cfg.Editor = config.EditorSettings{
			Cmd:  strings.TrimSpace(editorCmd.Text),
			Args: strings.TrimSpace(editorArgs.Text),
		}
		a.cfg.SetAISendCode(sendCode.Checked)
		a.cfg.SetAIVerify(aiEnabled.Checked)
		// 走统一入口：中栏的「列标尺」快捷勾选框也要同步过来（配置由下面统一落盘）
		a.setColumnRuler(showRuler.Checked, false)
		// 查找快捷键：格式不对时保留旧键位并明确报错，其余设置照常保存。
		newKey := strings.TrimSpace(searchKey.Text)
		keyErr := error(nil)
		if newKey == "" {
			a.cfg.SetSearchShortcut("")
		} else if _, err := config.ParseShortcut(newKey); err != nil {
			keyErr = err
		} else {
			a.cfg.SetSearchShortcut(newKey)
		}
		fontChanged := strings.TrimSpace(fontEntry.Text) != strings.TrimSpace(a.cfg.FontPath)
		a.cfg.FontPath = strings.TrimSpace(fontEntry.Text)
		a.engine.SetLLM(translate.NewLLMClient(toLLMConfig(a.cfg.LLM)))
		if err := a.cfg.Save(); err != nil {
			a.showError(err)
			return
		}
		if fontChanged {
			a.fontInfo = applyCJKTheme(a.fyneApp, a.cfg.FontPath)
			fontInfo.SetText(a.fontInfo.Report())
			a.statsLbl.SetText("设置已保存；字体已切换")
		} else {
			a.statsLbl.SetText("设置已保存")
		}
		// 快捷键改了就立刻重新注册（旧的先注销），不用重启程序。
		a.applySearchShortcut()
		if keyErr != nil {
			dialog.ShowError(fmt.Errorf(
				"查找快捷键格式有误，已保留原来的 %s。\n\n%v\n\n格式示例：Cmd+F、Ctrl+F、Ctrl+Shift+G、Alt+F3。",
				a.cfg.SearchShortcut(), keyErr), a.win)
		}
	}, a.win)
	d.Resize(fyne.NewSize(780, 760))
	d.Show()
}

// sectionLabel 生成设置表单里的分组标题。
func sectionLabel(text string) *widget.Label {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// showCorrection 显示译文纠错对话框。
func (a *App) showCorrection(kind, en, zh string) {
	enLabel := widget.NewLabel(en)
	enLabel.Wrapping = fyne.TextWrapWord
	enLabel.TextStyle = fyne.TextStyle{Monospace: true}
	zhEntry := widget.NewMultiLineEntry()
	zhEntry.SetText(zh)
	zhEntry.Wrapping = fyne.TextWrapWord
	zhEntry.SetMinRowsVisible(4)

	content := container.NewVBox(
		widget.NewLabelWithStyle("英文原文", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewVScroll(enLabel),
		widget.NewLabelWithStyle("中文译文（可修改，保存后长期生效）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		zhEntry,
	)
	d := dialog.NewCustomConfirm("纠错", "保存", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		text := strings.TrimSpace(zhEntry.Text)
		if text == "" {
			return
		}
		if err := a.engine.Correct(kind, en, text); err != nil {
			a.showError(err)
			return
		}
		if a.proj != nil && a.detail.current != nil {
			a.proj.Retranslate(a.detail.current)
			a.detail.Update(a.detail.current)
			a.refreshList()
			a.updateStats()
		}
		a.statsLbl.SetText("已保存译文，之后遇到同样内容会直接复用")
	}, a.win)
	d.Resize(fyne.NewSize(760, 520))
	d.Show()
}

// showMessageCorrection 纠正某条漏洞详情的中文翻译。
//
// 入库的键必须是「规范化模板」而不是原始英文：翻译时查询用的就是模板键，
// 若按原文入库，用户改了也不会被命中。
func (a *App) showMessageCorrection(f *model.Finding) {
	if f == nil {
		return
	}
	tmplLabel := widget.NewLabel(f.MessageKey)
	tmplLabel.Wrapping = fyne.TextWrapWord
	tmplLabel.TextStyle = fyne.TextStyle{Monospace: true}

	zhEntry := widget.NewMultiLineEntry()
	zhEntry.SetText(f.MessageZHTemplate)
	zhEntry.Wrapping = fyne.TextWrapWord
	zhEntry.SetMinRowsVisible(4)

	tip := widget.NewLabel("提示：请保留 {{LINK}}、{{N}}、{{W}}、{{API}} 这些占位符，" +
		"否则证据链位置链接会退化成文末的「相关位置」列表。")
	tip.Wrapping = fyne.TextWrapWord
	tip.Importance = widget.LowImportance

	rawLabel := widget.NewLabel(f.MessageRaw)
	rawLabel.Wrapping = fyne.TextWrapWord
	rawLabel.TextStyle = fyne.TextStyle{Monospace: true}

	content := container.NewVBox(
		widget.NewLabelWithStyle("英文模板（入库键）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewVScroll(tmplLabel),
		widget.NewLabelWithStyle("中文模板（可修改，保存后长期生效）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		zhEntry,
		tip,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("本次消息原文", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewVScroll(rawLabel),
	)
	d := dialog.NewCustomConfirm("纠正译文", "保存", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		text := strings.TrimSpace(zhEntry.Text)
		if text == "" {
			return
		}
		if err := a.engine.Correct(translate.KindMessage, f.MessageKey, text); err != nil {
			a.showError(err)
			return
		}
		a.proj.Retranslate(f)
		a.detail.Update(f)
		a.refreshList()
		a.updateStats()
		a.statsLbl.SetText("译文已保存，之后遇到同样的消息会直接复用")
	}, a.win)
	d.Resize(fyne.NewSize(820, 620))
	d.Show()
}

// showLibrary 显示翻译库浏览窗口。
func (a *App) showLibrary() {
	if a.engine.Store() == nil {
		dialog.ShowInformation("提示", "翻译库未初始化。", a.win)
		return
	}
	kindSel := widget.NewSelect([]string{"全部", "规则名称", "规则描述", "漏洞详情", "证据链标签"}, nil)
	kindSel.SetSelectedIndex(0)
	search := widget.NewEntry()
	search.SetPlaceHolder("搜索原文或译文")
	statLbl := widget.NewLabel("")
	var entries []translate.Entry
	var list *widget.List

	kindOf := func() string {
		switch kindSel.Selected {
		case "规则名称":
			return translate.KindRule
		case "规则描述":
			return translate.KindDescription
		case "漏洞详情":
			return translate.KindMessage
		case "证据链标签":
			return translate.KindLabel
		}
		return ""
	}

	reload := func() {
		rows, err := a.engine.Store().List(kindOf(), search.Text, 500)
		if err != nil {
			a.showError(err)
			return
		}
		entries = rows
		if list != nil {
			list.Refresh()
		}
		total, _ := a.engine.Store().Count("")
		statLbl.SetText(fmt.Sprintf("翻译库共 %d 条，当前显示 %d 条（按命中次数排序）", total, len(rows)))
	}
	kindSel.OnChanged = func(string) { reload() }
	search.OnChanged = func(string) { reload() }

	list = widget.NewList(
		func() int { return len(entries) },
		func() fyne.CanvasObject {
			// 模板行必须有初始文本：Fyne 只测量一次模板来决定固定行高
			en := widget.NewLabel("英文原文占位")
			en.TextStyle = fyne.TextStyle{Monospace: true}
			en.Truncation = fyne.TextTruncateEllipsis
			zh := widget.NewLabel("中文译文占位")
			zh.Truncation = fyne.TextTruncateEllipsis
			return container.NewVBox(en, zh)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if int(id) >= len(entries) {
				return
			}
			e := entries[id]
			box := o.(*fyne.Container)
			en := box.Objects[0].(*widget.Label)
			zh := box.Objects[1].(*widget.Label)
			tag := ""
			switch e.Origin {
			case translate.OriginBuiltin:
				tag = "内置"
			case translate.OriginLLM:
				tag = "大模型"
			default:
				tag = "人工"
			}
			en.SetText(fmt.Sprintf("[%s·命中%d] %s", tag, e.Hits, e.SourceEN))
			zh.SetText(e.SourceZH)
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(entries) {
			return
		}
		e := entries[id]
		list.UnselectAll()
		a.showCorrectionRecord(e, reload)
	}

	content := container.NewBorder(
		container.NewVBox(
			container.NewHBox(widget.NewLabel("类别"), kindSel, layout.NewSpacer()),
			search, statLbl, widget.NewSeparator(),
		),
		nil, nil, nil, list,
	)
	d := dialog.NewCustom("翻译库（中英对照，可复用）", "关闭", content, a.win)
	d.Resize(fyne.NewSize(900, 620))
	reload()
	d.Show()
}

// showCorrectionRecord 编辑翻译库中的某一条记录。
func (a *App) showCorrectionRecord(e translate.Entry, onSaved func()) {
	enLabel := widget.NewLabel(e.SourceEN)
	enLabel.Wrapping = fyne.TextWrapWord
	enLabel.TextStyle = fyne.TextStyle{Monospace: true}
	zhEntry := widget.NewMultiLineEntry()
	zhEntry.SetText(e.SourceZH)
	zhEntry.Wrapping = fyne.TextWrapWord
	zhEntry.SetMinRowsVisible(4)
	content := container.NewVBox(
		widget.NewLabelWithStyle("英文原文", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewVScroll(enLabel),
		widget.NewLabelWithStyle("中文译文", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		zhEntry,
	)
	d := dialog.NewCustomConfirm("编辑译文", "保存", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		if err := a.engine.Correct(e.Kind, e.SourceEN, strings.TrimSpace(zhEntry.Text)); err != nil {
			a.showError(err)
			return
		}
		if onSaved != nil {
			onSaved()
		}
		if a.proj != nil && a.detail.current != nil {
			a.proj.Retranslate(a.detail.current)
			a.detail.Update(a.detail.current)
		}
	}, a.win)
	d.Resize(fyne.NewSize(760, 460))
	d.Show()
}

// showMapping 显示路径映射工作台。
func (a *App) showMapping() {
	if a.proj == nil {
		dialog.ShowInformation("提示", "请先打开 CodeQL 结果 CSV 文件。", a.win)
		return
	}
	fromEntry := widget.NewEntry()
	fromEntry.SetPlaceHolder("例如 app 或 /home/runner/work/myproject")
	toEntry := widget.NewEntry()
	toEntry.SetPlaceHolder("例如 /Users/me/code/myproject")
	addBtn := widget.NewButtonWithIcon("添加前缀规则", theme.ContentAddIcon(), nil)

	unresolved := a.proj.UnresolvedFiles()
	infoLbl := widget.NewLabel(fmt.Sprintf(
		"共 %d 个文件、%d 条漏洞；当前有 %d 个文件未定位。点击「选择文件」手工指定即可长期生效。",
		a.proj.Stats.FileKinds, a.proj.Stats.Total, len(unresolved)))
	infoLbl.Wrapping = fyne.TextWrapWord

	var list *widget.List
	rows := unresolved
	refresh := func() {
		rows = a.proj.UnresolvedFiles()
		infoLbl.SetText(fmt.Sprintf(
			"共 %d 个文件、%d 条漏洞；当前有 %d 个文件未定位。",
			a.proj.Stats.FileKinds, a.proj.Stats.Total, len(rows)))
		if list != nil {
			list.Refresh()
		}
	}

	pickFor := func(csvPath string) {
		d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil {
				a.showError(err)
				return
			}
			if rc == nil {
				return
			}
			local := rc.URI().Path()
			_ = rc.Close()
			a.proj.SetOverride(csvPath, local)
			st := a.cfg.ProjectFor(a.proj.CSVPath, a.proj.SrcRoot)
			st.Overrides = a.proj.Resolver.Overrides()
			_ = a.cfg.Save()
			refresh()
			a.updateStats()
			if a.rows != nil && len(a.rows) > 0 {
				a.openFinding(a.rows[0], true)
			}
		}, a.win)
		showFileDialog(d, fyne.NewSize(900, 600))
	}

	addBtn.OnTapped = func() {
		from, to := strings.TrimSpace(fromEntry.Text), strings.TrimSpace(toEntry.Text)
		if from == "" || to == "" {
			dialog.ShowInformation("提示", "请同时填写 CSV 路径前缀与本地目录。", a.win)
			return
		}
		a.proj.AddPrefixRule(from, to)
		st := a.cfg.ProjectFor(a.proj.CSVPath, a.proj.SrcRoot)
		st.PrefixRules = a.proj.Resolver.PrefixRules()
		_ = a.cfg.Save()
		fromEntry.SetText("")
		toEntry.SetText("")
		refresh()
		a.updateStats()
	}

	// 两个输入框以前并排塞在 HBox 里，各自只拿到「内容最小宽度」——填到一半就看不全，
	// 长路径几乎没法编辑。改成 Form 的两行：输入框自动铺满对话框宽度，
	// 标签也把「CSV 前缀 → 本地目录」这层关系写清楚了。
	prefixForm := widget.NewForm(
		widget.NewFormItem("CSV 路径前缀", fromEntry),
		widget.NewFormItem("本地目录", toEntry),
	)

	list = widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			lbl.Truncation = fyne.TextTruncateEllipsis
			btn := widget.NewButton("选择文件", nil)
			return container.NewBorder(nil, nil, nil, btn, lbl)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if int(id) >= len(rows) {
				return
			}
			box := o.(*fyne.Container)
			var lbl *widget.Label
			var btn *widget.Button
			for _, obj := range box.Objects {
				switch v := obj.(type) {
				case *widget.Label:
					lbl = v
				case *widget.Button:
					btn = v
				}
			}
			if lbl == nil || btn == nil {
				return
			}
			csvPath := rows[id]
			lbl.SetText(csvPath)
			btn.OnTapped = func() { pickFor(csvPath) }
		},
	)

	head := container.NewVBox(
		infoLbl,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("前缀规则（把 CSV 路径前缀替换成本地目录）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, addBtn, prefixForm),
		widget.NewSeparator(),
		widget.NewLabelWithStyle("未定位的文件", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	d := dialog.NewCustom("路径映射", "关闭", container.NewBorder(head, nil, nil, nil, list), a.win)
	d.Resize(fyne.NewSize(940, 640))
	refresh()
	d.Show()
}
