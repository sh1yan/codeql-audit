package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/config"
	"codeql-audit/internal/model"
	"codeql-audit/internal/project"
	"codeql-audit/internal/sourcefile"
	"codeql-audit/internal/translate"
	"codeql-audit/internal/verify"
)

// App 是工具主界面。
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	cfg     *config.Config
	engine  *translate.Engine
	proj    *project.Project

	// 左栏
	list       *widget.List
	rows       []*model.Finding
	search     *widget.Entry
	ruleSel    *widget.Select
	fileSel    *widget.Select
	sevSel     *widget.Select
	onlyUnread *widget.Check
	// unresolvedChk 是「查看未定位」勾选框：只留下沉淀点还没在本地源码里
	// 定位成功的条目，方便集中处理「路径映射」。
	unresolvedChk *widget.Check
	// aiSel 是 AI 判定筛选下拉：全部 / 误报 / 确认 / 存疑 / 未验证。
	aiSel    *widget.Select
	countLbl *widget.Label
	statsLbl *widget.Label

	// 中栏
	codeView *CodeView
	fileLbl  *widget.Label
	navLbl   *widget.Label
	backBtn  *widget.Button
	findBtn  *widget.Button
	editBtn  *widget.Button
	// searchShortcut 记录当前已注册的「在当前代码文件中查找」快捷键，
	// 用户改键位时要先注销旧的，避免新旧两个快捷键同时生效（见 shortcut.go）。
	searchShortcut fyne.Shortcut
	// shortcutCanvas 是注册快捷键的目标画布；正常运行时为 nil（表示直接用窗口画布），
	// 测试会塞一个记录调用的假画布进来，断言新旧键位的注册/注销。
	shortcutCanvas shortcutCanvas
	// searchMenuItem 是「编辑」菜单里的查找菜单项（主菜单快捷键比画布快捷键更可靠）。
	searchMenuItem *fyne.MenuItem
	// rulerChk 是中栏标题栏上的列标尺快捷开关（与设置里的开关同步）。
	rulerChk *widget.Check
	// centerHead 是中栏标题栏容器：提示行显隐后必须让它重排一次，
	// 下面的代码视图才会立刻拿回那一行的高度（见 setNavHint）。
	centerHead *fyne.Container

	// 右栏
	detail *DetailPane

	// 顶栏
	langSel *widget.Select
	locLbl  *widget.Label
	// infoBtn 控制顶栏项目信息区的折叠/展开。
	infoBtn *widget.Button

	// 三栏布局
	split      *container.Split // 左栏 | （中栏 + 右栏）
	inner      *container.Split // 中栏 | 右栏
	leftPane   fyne.CanvasObject
	codePane   fyne.CanvasObject
	detailPane fyne.CanvasObject

	// 中文字体探测结果
	fontInfo FontInfo

	// 状态
	fileCache map[string][]string
	encCache  map[string]string
	history   []navPoint
	ctxSink   model.Location
	ctxSource []model.Location
	curLine   int
	lang      string
	ruleNames []string
	ruleKeys  []string
	fileName  []string
	fileKeys  []string

	// 只读状态与 AI 判定
	reviewed map[string]bool
	verdicts map[string]config.VerdictRecord

	// 配置落盘防抖
	saveMu    sync.Mutex
	saveTimer *time.Timer
	closing   bool

	// snippetMu 保护后台 AI 验证读取源码片段时的缓存
	snippetMu    sync.Mutex
	snippetCache map[string][]string
}

type navPoint struct {
	path    string
	line    int
	sink    model.Location
	sources []model.Location
}

// Run 启动图形界面。
func Run(cfg *config.Config) error {
	a, err := newApp(app.NewWithID("com.codeqlaudit.app"), cfg)
	if err != nil {
		return err
	}
	a.openLastProject()
	a.win.ShowAndRun()
	return nil
}

// newApp 组装界面但不进入事件循环。传入不同的 fyne.App 即可在测试驱动下运行。
func newApp(fyneApp fyne.App, cfg *config.Config) (*App, error) {
	a := &App{
		fyneApp:      fyneApp,
		cfg:          cfg,
		fileCache:    map[string][]string{},
		encCache:     map[string]string{},
		snippetCache: map[string][]string{},
		reviewed:     map[string]bool{},
		verdicts:     map[string]config.VerdictRecord{},
		lang:         cfg.DisplayLang,
	}
	if a.lang == "" {
		a.lang = "zh"
	}
	store, err := translate.OpenStore(cfg.TranslateDB)
	if err != nil {
		return nil, fmt.Errorf("打开翻译库失败：%w", err)
	}
	engine, err := translate.NewEngine(store, translate.NewLLMClient(toLLMConfig(cfg.LLM)))
	if err != nil {
		return nil, err
	}
	a.engine = engine
	// 必须在创建控件之前注入中文字体，否则文本度量会沿用 Fyne 内置字体
	a.fontInfo = applyCJKTheme(fyneApp, cfg.FontPath)

	a.win = a.fyneApp.NewWindow("CodeQL 漏洞审计工具 - shiyan")
	a.win.Resize(fyne.NewSize(cfg.WindowWidth, cfg.WindowHeight))
	a.win.SetContent(a.buildUI())
	// 必须在 buildUI 之后：快捷键要挂到窗口画布上，并按配置更新查找框提示。
	a.applySearchShortcut()
	a.win.SetOnClosed(func() {
		a.saveWindowState()
		a.stopSaveTimer()
		a.flushProjectState()
		_ = a.cfg.Save()
		_ = store.Close()
	})
	return a, nil
}

// saveWindowState 把窗口尺寸与三栏间距写回配置（关闭窗口时调用）。
//
// 间距要单独存一份：用户把右栏拖到自己顺手的宽度以后，下次启动不该又跳回默认值。
func (a *App) saveWindowState() {
	if a.win != nil {
		a.cfg.WindowWidth = a.win.Canvas().Size().Width
		a.cfg.WindowHeight = a.win.Canvas().Size().Height
	}
	if a.split != nil {
		a.cfg.SplitLeft = a.split.Offset
	}
	if a.inner != nil {
		a.cfg.SplitDetail = a.inner.Offset
	}
	a.cfg.DisplayLang = a.lang
}

func toLLMConfig(s config.LLMSettings) translate.LLMConfig {
	return translate.LLMConfig{
		Enabled:        s.Enabled,
		BaseURL:        s.BaseURL,
		APIKey:         s.APIKey,
		Model:          s.Model,
		TimeoutSeconds: s.TimeoutSeconds,
		TemplateOnly:   s.TemplateOnly,
	}
}

func (a *App) buildUI() fyne.CanvasObject {
	// ---------- 顶栏 ----------
	openCSV := widget.NewButtonWithIcon("打开结果 CSV", theme.FolderOpenIcon(), a.chooseCSV)
	openSrc := widget.NewButtonWithIcon("选择源码目录", theme.FolderIcon(), a.chooseSrcDir)
	exportBtn := widget.NewButtonWithIcon("导出中文 CSV", theme.DocumentSaveIcon(), a.exportCSV)
	reportBtn := widget.NewButtonWithIcon("导出审计报告", theme.DocumentCreateIcon(), a.exportReport)
	libBtn := widget.NewButtonWithIcon("翻译库", theme.ListIcon(), a.showLibrary)
	mapBtn := widget.NewButtonWithIcon("路径映射", theme.NavigateNextIcon(), a.showMapping)
	verifyAllBtn := widget.NewButtonWithIcon("批量 AI 验证", theme.ConfirmIcon(), a.showBatchVerify)
	// 「查找」放在顶栏而不是中栏标题栏：中栏标题栏右侧那一排按钮的宽度会直接
	// 变成中栏的最小宽度，从而把「右栏能拖多宽」压小（layout_test.go 里有回归测试）。
	// 顶栏还有 200px 以上余量，放这里既不影响三栏间距，也和其它全局动作按钮一致。
	a.findBtn = widget.NewButtonWithIcon("查找代码", theme.SearchIcon(), a.openCodeSearch)
	setBtn := widget.NewButtonWithIcon("设置", theme.SettingsIcon(), a.showSettings)
	a.langSel = widget.NewSelect([]string{"中文", "英文", "中英对照"}, func(s string) {
		switch s {
		case "英文":
			a.lang = "en"
		case "中英对照":
			a.lang = "both"
		default:
			a.lang = "zh"
		}
		a.cfg.DisplayLang = a.lang
		// 注意：SetSelected 会立即触发回调，此时右栏可能还没创建
		if a.detail != nil {
			a.detail.SetLang(a.lang)
		}
	})
	a.langSel.SetSelected(langLabel(a.lang))
	a.locLbl = widget.NewLabel("尚未载入结果文件。请点击「打开结果 CSV」开始。")
	a.locLbl.Wrapping = fyne.TextWrapWord
	a.statsLbl = widget.NewLabel("")
	a.statsLbl.Wrapping = fyne.TextWrapWord

	// 顶栏项目信息（结果文件 / 源码目录 / 字体）可以整块折叠起来，
	// 折叠后只剩工具栏一行，中栏能多出两行代码的高度。
	a.infoBtn = widget.NewButtonWithIcon("", theme.MenuDropUpIcon(), a.toggleInfo)
	a.infoBtn.Importance = widget.LowImportance

	top := container.NewVBox(
		container.NewHBox(openCSV, openSrc, widget.NewSeparator(), exportBtn, reportBtn,
			widget.NewSeparator(), libBtn, mapBtn, verifyAllBtn, a.findBtn, setBtn,
			layout.NewSpacer(), a.infoBtn, widget.NewLabel("语言："), a.langSel),
		a.locLbl,
	)
	a.setInfoCollapsed(a.cfg.TopInfoCollapsed)

	// ---------- 左栏 ----------
	a.search = widget.NewEntry()
	a.search.SetPlaceHolder("搜索规则 / 文件 / 消息内容")
	a.search.OnChanged = func(string) { a.refreshList() }

	a.ruleSel = widget.NewSelect([]string{"全部规则"}, func(string) { a.refreshList() })
	a.fileSel = widget.NewSelect([]string{"全部文件"}, func(string) { a.refreshList() })
	a.sevSel = widget.NewSelect([]string{"全部级别"}, func(string) { a.refreshList() })
	a.onlyUnread = widget.NewCheck("只看未读", func(bool) { a.refreshList() })
	// 「查看未定位」：未定位的条目要等路径映射处理完才会消失，单独挑出来看比
	// 在列表里一条条找 [未定位] 省事。
	a.unresolvedChk = widget.NewCheck("查看未定位", func(bool) { a.refreshList() })
	// AI 判定筛选：以前只有一个「只看 AI 误报」勾选框，无法单独看「AI 确认 / AI 存疑 / 未验证」。
	a.aiSel = widget.NewSelect(aiVerdictOptions, func(string) { a.refreshList() })
	a.aiSel.SetSelectedIndex(0)
	markAllBtn := widget.NewButton("全部标为已读", a.markAllReviewed)
	resetReadBtn := widget.NewButton("重置已读", a.resetReviewed)
	readLegend := widget.NewLabel("蓝色竖条＝未读，灰字＋「已读」＝看过")
	readLegend.Importance = widget.LowImportance
	readLegend.Wrapping = fyne.TextWrapWord
	a.countLbl = widget.NewLabel("共 0 条")
	a.countLbl.Importance = widget.LowImportance
	a.countLbl.Wrapping = fyne.TextWrapWord

	filters := widget.NewAccordion(
		widget.NewAccordionItem("筛选", container.NewVBox(
			a.ruleSel,
			a.fileSel,
			a.sevSel,
			container.NewHBox(a.onlyUnread, a.unresolvedChk),
			a.aiSel,
			container.NewHBox(markAllBtn, resetReadBtn),
			readLegend,
		)),
	)
	filters.Open(0)

	a.list = widget.NewList(
		func() int { return len(a.rows) },
		func() fyne.CanvasObject { return newFindingRow() },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if int(id) < len(a.rows) {
				row := o.(*findingRow)
				row.id = int(id)
				row.onTapped = a.tapFinding
				f := a.rows[id]
				row.Set(f)
				row.SetRead(a.isReviewed(f))
			}
		},
	)
	a.list.OnSelected = func(id widget.ListItemID) {
		if int(id) >= 0 && int(id) < len(a.rows) {
			a.openFindingAt(int(id), true)
		}
	}

	left := container.NewBorder(
		container.NewVBox(
			container.NewPadded(a.search),
			filters,
			container.NewPadded(a.countLbl),
		),
		nil, nil, nil,
		a.list,
	)

	// ---------- 中栏 ----------
	a.fileLbl = widget.NewLabel("未打开文件")
	a.fileLbl.TextStyle = fyne.TextStyle{Bold: true}
	a.fileLbl.Truncation = fyne.TextTruncateEllipsis
	a.navLbl = widget.NewLabel("")
	a.navLbl.Importance = widget.LowImportance
	a.navLbl.Wrapping = fyne.TextWrapWord
	a.backBtn = widget.NewButtonWithIcon("返回上一次跳转", theme.NavigateBackIcon(), a.goBack)
	a.backBtn.Disable()
	a.editBtn = widget.NewButtonWithIcon("用编辑器打开", theme.ComputerIcon(), a.openInEditor)
	// 中栏快捷开关：列标尺以前只能进「设置」里改，来回一趟才能看到效果。
	// 这里放一个同名勾选框，与设置里的开关共用 App.setColumnRuler。
	a.rulerChk = widget.NewCheck("列标尺", func(on bool) { a.setColumnRuler(on, true) })
	a.rulerChk.SetChecked(a.cfg.ColumnRuler())
	centerHead := container.NewVBox(
		container.NewBorder(nil, nil, nil,
			container.NewHBox(a.rulerChk, a.backBtn, a.editBtn), a.fileLbl),
		a.navLbl,
		widget.NewSeparator(),
	)
	a.centerHead = centerHead
	a.codeView = NewCodeView()
	a.codeView.SetColumnRuler(a.cfg.ColumnRuler())
	// 代码区复制成功后在状态栏给个回执：用户才知道 Cmd/Ctrl+C 到底有没有生效。
	a.codeView.SetOnCopy(func(lines int) {
		if lines <= 1 {
			a.statsLbl.SetText("已复制代码到剪贴板")
			return
		}
		a.statsLbl.SetText(fmt.Sprintf("已复制 %d 行代码到剪贴板", lines))
	})
	center := container.NewBorder(centerHead, nil, nil, nil, a.codeView.Object())

	// ---------- 右栏 ----------
	a.detail = NewDetailPane()
	a.detail.lang = a.lang
	a.detail.onLink = a.jumpToLink
	a.detail.onJump = func(loc model.Location, kind HLKind) { a.jumpToLocation(loc, kind, "") }
	a.detail.onCorrect = a.showMessageCorrection
	a.detail.onCopy = a.copyCurrentMessage
	a.detail.onVerify = a.verifyFinding
	a.detail.onClearVerdict = a.clearVerdict
	a.detail.Update(nil)

	inner := container.NewHSplit(center, a.detail.Object())
	split := container.NewHSplit(left, inner)
	a.split, a.inner = split, inner
	// 恢复用户上次拖出来的间距；没有记录时用默认值。
	split.SetOffset(savedRatio(a.cfg.SplitLeft, defaultSplitLeft))
	inner.SetOffset(savedRatio(a.cfg.SplitDetail, defaultSplitDetail))
	a.leftPane, a.codePane, a.detailPane = left, center, a.detail.Object()

	bottom := container.NewVBox(widget.NewSeparator(), container.NewPadded(a.statsLbl))
	root := container.NewBorder(top, bottom, nil, nil, split)
	// 提示行默认收起：标题栏只占「文件信息 + 按钮」这一行，代码区多出一行半。
	a.setNavHint("")
	return root
}

func langLabel(lang string) string {
	switch lang {
	case "en":
		return "英文"
	case "both":
		return "中英对照"
	}
	return "中文"
}

// toggleInfo 折叠/展开顶栏的项目信息区。
func (a *App) toggleInfo() {
	// 当前可见 = 还没折叠，点一下就应该折叠
	a.cfg.TopInfoCollapsed = a.locLbl.Visible()
	a.setInfoCollapsed(a.cfg.TopInfoCollapsed)
	a.saveNow()
}

// setInfoCollapsed 应用折叠状态；折叠时只留工具栏，展开时显示项目信息。
func (a *App) setInfoCollapsed(collapsed bool) {
	if a.infoBtn == nil || a.locLbl == nil {
		return
	}
	if collapsed {
		a.locLbl.Hide()
		a.infoBtn.SetText("展开信息")
		a.infoBtn.SetIcon(theme.MenuDropDownIcon())
	} else {
		a.locLbl.Show()
		a.infoBtn.SetText("收起信息")
		a.infoBtn.SetIcon(theme.MenuDropUpIcon())
	}
	a.infoBtn.Refresh()
	a.locLbl.Refresh()
}

// setColumnRuler 统一处理列标尺开关：中栏快捷勾选框「列标尺」与
// 「设置 → 代码视图 → 显示列标尺」都走这里，保证两处状态、配置与界面一致。
//
// persist 为 true 时立刻落盘（中栏快捷开关用），设置对话框自己保存配置时传 false。
// 注意 widget.Check.SetChecked 会触发 OnChanged，所以先比较再设置，避免回调递归。
func (a *App) setColumnRuler(on bool, persist bool) {
	if a.cfg != nil && a.cfg.ColumnRuler() != on {
		a.cfg.SetColumnRuler(on)
	}
	if a.codeView != nil {
		a.codeView.SetColumnRuler(on)
		a.codeView.RefreshRuler(a.curLine)
	}
	if a.rulerChk != nil && a.rulerChk.Checked != on {
		a.rulerChk.SetChecked(on)
	}
	if persist {
		a.saveNow()
	}
}

// setNavHint 更新中栏标题栏里的跳转提示行。
//
// 提示行平时是收起来的：Fyne 的 Label 最小高度固定是 37px（文字 21px + 上下各 8px
// 内边距），多挂一行就等于少看一行半代码；而「跳到哪一行」在代码视图里本来就是
// 一眼就能看到的（高亮行会滚到视口正中）。所以只有真正需要用户处理的提示
// （未定位、返回失败、没有可返回的历史等）才展开这一行。
//
// 收放之后必须主动重排：Fyne 的 Hide/Show 只会重绘控件本身，父容器的布局不会
// 自动重跑（同 CodeView.SetColumnRuler 的注释），只隐藏不重排的话代码视图上方会
// 留下一条什么都没画的空白。这里重排「标题栏自身」和「外层 Border」，
// 后者才决定代码视图能拿回多少高度。
func (a *App) setNavHint(text string) {
	if a.navLbl == nil {
		return
	}
	text = strings.TrimSpace(text)
	want := text != ""
	if a.navLbl.Text == text && a.navLbl.Visible() == want {
		return // 什么都没变，不必触发一次重排
	}
	if want {
		a.navLbl.Show()
	} else {
		a.navLbl.Hide()
	}
	a.navLbl.SetText(text)
	if a.centerHead != nil {
		a.centerHead.Refresh()
	}
	if a.codePane != nil {
		a.codePane.Refresh()
	}
}

// 三栏默认间距：左栏 24%，剩下的中/右两栏各占一半。//
// 中/右各半是个平衡的起点：右栏在 1480 宽的窗口里约 555px，正文换行后每行
// 二十多个汉字，证据链按钮也基本放得下（放不下时左对齐裁掉结尾几个字）。
// 用户拖过之后就会按拖出来的间距持久化，见 saveWindowState。
const (
	defaultSplitLeft   = 0.24
	defaultSplitDetail = 0.50
)

// savedRatio 返回可用的历史间距；缺失或明显非法时退回默认值。
func savedRatio(v, def float64) float64 {
	if v <= 0.02 || v >= 0.98 {
		return def
	}
	return v
}

// ------------------------- 载入项目 -------------------------

func (a *App) openLastProject() {
	st := a.cfg.Last()
	if st == nil {
		return
	}
	if _, err := os.Stat(st.CSVPath); err != nil {
		return
	}
	a.loadProject(st.CSVPath, st.SrcRoot, false)
}

func (a *App) chooseCSV() {
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil {
			a.showError(err)
			return
		}
		if rc == nil {
			return
		}
		path := rc.URI().Path()
		_ = rc.Close()
		a.cfg.LastProject = path
		st := a.cfg.ProjectFor(path, "")
		a.loadProject(path, st.SrcRoot, true)
	}, a.win)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
	showFileDialog(d, fyne.NewSize(940, 620))
}

func (a *App) chooseSrcDir() {
	if a.cfg.LastProject == "" {
		dialog.ShowInformation("提示", "请先打开 CodeQL 结果 CSV 文件。", a.win)
		return
	}
	d := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil {
			a.showError(err)
			return
		}
		if lu == nil {
			return
		}
		root := lu.Path()
		if a.proj == nil {
			a.loadProject(a.cfg.LastProject, root, true)
			return
		}
		a.proj.SetSrcRoot(root)
		st := a.cfg.ProjectFor(a.proj.CSVPath, root)
		st.SrcRoot = root
		_ = a.cfg.Save()
		a.refreshList()
		a.updateStats()
		if a.rows != nil && len(a.rows) > 0 {
			a.openFinding(a.rows[0], true)
		}
	}, a.win)
	showFileDialog(d, fyne.NewSize(940, 620))
}

func (a *App) loadProject(csvPath, srcRoot string, announce bool) {
	if strings.TrimSpace(csvPath) == "" {
		return
	}
	progress := widget.NewProgressBarInfinite()
	msg := widget.NewLabel("正在解析、翻译并定位源码 …")
	dlg := dialog.NewCustomWithoutButtons("正在载入项目", container.NewVBox(msg, progress), a.win)
	dlg.Resize(fyne.NewSize(460, 160))
	dlg.Show()

	go func() {
		err := a.doLoad(csvPath, srcRoot, func(m string) {
			fyne.Do(func() { msg.SetText(m) })
		})
		fyne.Do(func() {
			dlg.Hide()
			if err != nil {
				a.showError(err)
				return
			}
			a.applyProject(announce)
		})
	}()
}

// doLoad 同步载入项目（供界面与测试共用）。
func (a *App) doLoad(csvPath, srcRoot string, progress func(string)) error {
	st := a.cfg.ProjectFor(csvPath, srcRoot)
	if progress == nil {
		progress = func(string) {}
	}
	p, err := project.Load(project.Options{
		CSVPath:     csvPath,
		SrcRoot:     st.SrcRoot,
		PrefixRules: st.PrefixRules,
		Overrides:   st.Overrides,
		Engine:      a.engine,
		Progress: func(done, total int, m string) {
			progress(m)
		},
	})
	if err != nil {
		return err
	}
	a.proj = p
	a.cfg.LastProject = csvPath
	_ = a.cfg.Save()
	return nil
}

func (a *App) applyProject(announce bool) {
	p := a.proj
	a.fileCache = map[string][]string{}
	a.encCache = map[string]string{}
	a.snippetCache = map[string][]string{}
	a.history = nil
	a.ctxSink, a.ctxSource = model.Location{}, nil
	a.codeView.Clear()
	a.fileLbl.SetText("未打开文件")
	a.setNavHint("")
	a.updateNavButtons()

	// 恢复该项目的已读状态与 AI 判定
	a.loadProjectStateFrom(p.CSVPath)
	a.applyVerdicts()

	a.locLbl.SetText(fmt.Sprintf("结果文件：%s\n源码目录：%s\n%s",
		p.CSVPath, orNone(p.SrcRoot), a.fontInfo.Report()))

	// 重建筛选下拉
	a.ruleNames = []string{"全部规则"}
	a.ruleKeys = []string{""}
	for _, r := range p.Rules() {
		name := r
		if zh := p.Stats.RuleNameZH[r]; zh != "" {
			name = zh + "　(" + r + ")"
		}
		a.ruleNames = append(a.ruleNames, name)
		a.ruleKeys = append(a.ruleKeys, r)
	}
	a.ruleSel.Options = a.ruleNames
	a.ruleSel.SetSelectedIndex(0)

	a.fileName = []string{"全部文件"}
	a.fileKeys = []string{""}
	for _, f := range p.Files() {
		a.fileName = append(a.fileName, f)
		a.fileKeys = append(a.fileKeys, f)
	}
	a.fileSel.Options = a.fileName
	a.fileSel.SetSelectedIndex(0)

	sevOpts := []string{"全部级别"}
	for _, s := range p.SortedSeverities() {
		sevOpts = append(sevOpts, model.Severity(s).ZH())
	}
	a.sevSel.Options = sevOpts
	a.sevSel.SetSelectedIndex(0)

	a.refreshList()
	a.updateStats()

	if len(p.Warnings) > 0 && announce {
		dialog.ShowInformation("解析提示", strings.Join(limitStrings(p.Warnings, 8), "\n"), a.win)
	}
	if len(a.rows) > 0 {
		a.list.Select(0)
		a.openFindingAt(0, true)
	}
}

// applyVerdicts 把持久化的 AI 判定回填到漏洞对象上。
func (a *App) applyVerdicts() {
	if a.proj == nil {
		return
	}
	for _, f := range a.proj.Findings {
		if rec, ok := a.verdicts[f.Key()]; ok {
			f.AIVerdict = rec.Status
			f.AIReason = rec.Reason
			f.AIConfidence = rec.Confidence
			f.AIModel = rec.Model
			f.AIAt = rec.At
		} else {
			f.AIVerdict, f.AIReason, f.AIConfidence, f.AIModel, f.AIAt = "", "", 0, "", ""
		}
	}
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（未设置，源码跳转不可用）"
	}
	return s
}

func limitStrings(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return append(in[:n], fmt.Sprintf("…… 还有 %d 条", len(in)-n))
}

func (a *App) refreshList() {
	// 构建界面时 SetSelectedIndex 会立刻触发回调，此时列表/计数标签可能还没创建。
	if a.list == nil || a.countLbl == nil {
		return
	}
	if a.proj == nil {
		a.rows = nil
		a.list.Refresh()
		a.countLbl.SetText("共 0 条")
		return
	}
	f := project.Filter{
		Keyword:        a.search.Text,
		OnlyUnresolved: a.unresolvedChk != nil && a.unresolvedChk.Checked,
	}
	if i := a.ruleSel.SelectedIndex(); i > 0 && i < len(a.ruleKeys) {
		f.Rule = a.ruleKeys[i]
	}
	if i := a.fileSel.SelectedIndex(); i > 0 && i < len(a.fileKeys) {
		f.File = a.fileKeys[i]
	}
	if i := a.sevSel.SelectedIndex(); i > 0 {
		f.Severity = strings.ToLower(sevKeyOf(a.sevSel.Selected))
	}
	rows := a.proj.Apply(f)
	// 已读 / AI 判定属于界面侧状态，放在 project.Apply 之后过滤
	verdict := a.verdictFilter()
	if a.onlyUnread.Checked || verdict != "" {
		filtered := make([]*model.Finding, 0, len(rows))
		for _, fd := range rows {
			if a.onlyUnread.Checked && a.isReviewed(fd) {
				continue
			}
			if verdict != "" && !matchVerdict(fd, verdict) {
				continue
			}
			filtered = append(filtered, fd)
		}
		rows = filtered
	}
	a.rows = rows
	a.list.Refresh()

	unread := 0
	for _, fd := range a.proj.Findings {
		if !a.isReviewed(fd) {
			unread++
		}
	}
	falsePos, confirmed, unsure := a.verdictCounts()
	a.countLbl.SetText(fmt.Sprintf("显示 %d / %d 条　·　未读 %d　·　AI：误报 %d / 确认 %d / 存疑 %d",
		len(a.rows), a.proj.Stats.Total, unread, falsePos, confirmed, unsure))
}

// aiVerdictOptions 是左栏「AI 判定」筛选下拉的选项。
var aiVerdictOptions = []string{"全部 AI 判定", "AI 误报", "AI 确认", "AI 存疑", "未验证"}

// aiVerdictNone 表示「尚未做过 AI 判定」这一档筛选。
const aiVerdictNone = "__none__"

// aiVerdictFilters 与 aiVerdictOptions 一一对应：空串表示不筛选。
var aiVerdictFilters = []string{
	"",
	string(verify.FalsePositive),
	string(verify.TruePositive),
	string(verify.Uncertain),
	aiVerdictNone,
}

// verdictFilter 返回当前选中的 AI 判定筛选条件（空串表示不筛选）。
func (a *App) verdictFilter() string {
	if a.aiSel == nil {
		return ""
	}
	i := a.aiSel.SelectedIndex()
	if i <= 0 || i >= len(aiVerdictFilters) {
		return ""
	}
	return aiVerdictFilters[i]
}

// matchVerdict 判断一条漏洞是否符合 AI 判定筛选条件。
func matchVerdict(f *model.Finding, want string) bool {
	if f == nil {
		return false
	}
	if want == aiVerdictNone {
		return !f.HasAIVerdict()
	}
	return f.AIVerdict == want
}

func sevKeyOf(zh string) string {
	switch zh {
	case "严重":
		return "error"
	case "警告":
		return "warning"
	case "提示":
		return "note"
	case "建议":
		return "recommendation"
	}
	return ""
}

func (a *App) updateStats() {
	if a.proj == nil {
		a.statsLbl.SetText("")
		return
	}
	s := a.proj.Stats
	tr := a.engine.Stats()
	unread := 0
	for _, f := range a.proj.Findings {
		if !a.isReviewed(f) {
			unread++
		}
	}
	falsePos, confirmed, unsure := a.verdictCounts()
	verified := falsePos + confirmed + unsure
	a.statsLbl.SetText(fmt.Sprintf(
		"共 %d 条漏洞　·　规则 %d 类　·　涉及文件 %d 个　·　已定位源码 %d/%d　·　未读 %d　·　AI 已验证 %d（误报 %d / 确认 %d / 存疑 %d）　·　中文命中 内置 %d / 库 %d / 大模型 %d　·　待翻译 %d",
		s.Total, s.RuleKinds, s.FileKinds, s.Resolved, s.Total, unread,
		verified, falsePos, confirmed, unsure,
		tr.Builtin, tr.Store, tr.LLM, s.Pending))
}

// ------------------------- 跳转与高亮 -------------------------

// tapFinding 处理左侧列表的行点击。
//
// Fyne 的 List.Select 对「已经是当前选中项」的 id 会直接返回、不再触发 OnSelected，
// 因此重复点击同一行（例如代码视图被滚走或清空后想再看一眼）时必须手动重新定位，
// 否则点了没反应。
func (a *App) tapFinding(id int) {
	if id < 0 || id >= len(a.rows) {
		return
	}
	a.list.Select(id) // 切换选中态；同一行时 Fyne 不会回调，所以下面再定位一次
	a.openFindingAt(id, true)
}

// openFindingAt 打开列表第 id 条漏洞（含已读标记与详情刷新）。
func (a *App) openFindingAt(id int, jump bool) {
	if id < 0 || id >= len(a.rows) {
		return
	}
	a.openFinding(a.rows[id], jump)
}

func (a *App) openFinding(f *model.Finding, jump bool) {
	if f == nil {
		return
	}
	a.detail.Update(f)
	a.detail.SetActive("")
	a.markReviewed(f)
	if !jump {
		return
	}
	a.ctxSink = f.Sink
	a.ctxSource = f.Sources
	a.history = nil
	a.updateNavButtons()
	if !a.showLocation(f.Sink, f.Sink.StartLine) {
		a.setNavHint(a.unresolvedHint(f.Sink))
		a.codeView.Clear()
	} else {
		a.setNavHint("")
	}
	a.detail.SetActive("sink")
}

// jumpToLocation 跳转到某个位置。
//
// 关键点：目标文件必须由 **loc 自己** 决定，而不是当前沉淀点所在的文件。
// 之前这里用 ctxSink 的路径去开会话文件，导致点击「来源 1」（例如 admin.py:3）
// 时打开的却是沉淀点所在的 sql.py，并且把来源的行号套到了 sql.py 上，
// 于是「证据链的行数看起来是错的」。
//
// note 非空时才会在标题栏展开提示行；普通跳转传空串即可——文件标题与居中高亮的
// 代码行已经说明了跳到哪，不必再占用一行高度（见 setNavHint）。
func (a *App) jumpToLocation(loc model.Location, kind HLKind, note string) {
	if a.codeView.Path() != "" {
		a.history = append(a.history, navPoint{
			path: a.codeView.Path(), line: a.curLine,
			sink: a.ctxSink, sources: a.ctxSource,
		})
	}
	if kind == HLSource {
		a.ctxSource = prependUnique(a.ctxSource, loc)
	}
	a.updateNavButtons()
	if !a.showLocation(loc, loc.StartLine) {
		a.setNavHint(a.unresolvedHint(loc))
		return
	}
	a.setNavHint(note)
}

// prependUnique 把一处位置放到高亮列表最前面（已存在则先移除，避免反复点击后堆叠）。
func prependUnique(list []model.Location, loc model.Location) []model.Location {
	out := make([]model.Location, 0, len(list)+1)
	out = append(out, loc)
	for _, l := range list {
		if sameLocation(l, loc) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func sameLocation(a, b model.Location) bool {
	return a.File == b.File && a.StartLine == b.StartLine && a.StartCol == b.StartCol
}

func (a *App) jumpToLink(l *model.LinkRef) {
	if l == nil || a.proj == nil {
		return
	}
	path, line, ok := a.proj.ResolveLink(l)
	if !ok {
		dialog.ShowInformation("无法定位",
			fmt.Sprintf("在源码目录中找不到该位置对应的文件：\n%s\n\n可在「路径映射」中手工指定。", l.File), a.win)
		return
	}
	loc := l.Location()
	loc.ResolvedPath = path
	loc.StartLine = line
	a.jumpToLocation(loc, HLSource, "已跳转到漏洞详情中的链接位置")
}

func (a *App) goBack() {
	if len(a.history) == 0 {
		a.setNavHint("没有可返回的跳转记录：每次点击漏洞或证据链才会产生一条历史。")
		return
	}
	last := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.ctxSink = last.sink
	a.ctxSource = last.sources
	a.updateNavButtons()
	if !a.showAt(last.path, last.line) {
		a.setNavHint("返回失败：原文件已不可读（" + last.path + "）")
		return
	}
	a.setNavHint("")
}

// updateNavButtons 根据历史记录决定「返回上一次跳转」是否可用。
func (a *App) updateNavButtons() {
	if a.backBtn == nil {
		return
	}
	if len(a.history) == 0 {
		a.backBtn.Disable()
		return
	}
	a.backBtn.Enable()
}

// unresolvedHint 生成「无法定位」的提示文案。
func (a *App) unresolvedHint(loc model.Location) string {
	return fmt.Sprintf("未定位：%s（可在「路径映射」里手工指定本地文件）", loc.File)
}

// showLocation 打开 loc 指向的文件，并把 line 行居中高亮。
//
// 定位失败时不会清空代码视图：多数情况下用户只是点了一个「未定位」的来源，
// 把当前正在看的代码清掉反而更糟；调用方负责提示。
func (a *App) showLocation(loc model.Location, line int) bool {
	path := loc.ResolvedPath
	if path == "" {
		path = a.resolvePath(loc.File)
	}
	if path == "" {
		a.fileLbl.SetText("未定位：" + loc.File)
		return false
	}
	if line <= 0 {
		line = loc.StartLine
	}
	return a.showAt(path, line)
}

// showAt 打开指定文件并把目标行滚动到视口中央。
func (a *App) showAt(path string, line int) bool {
	lines, enc, ok := a.readFile(path)
	if !ok {
		return false
	}
	if a.codeView.Path() != path {
		a.codeView.SetFile(path, enc, lines)
	}
	a.rebuildHighlights()
	if line <= 0 {
		line = 1
	}
	want := line
	if line > len(lines) {
		line = len(lines)
	}
	a.curLine = line
	a.codeView.ScrollToLineCentered(line)
	a.codeView.RefreshRuler(line)
	note := ""
	if want > len(lines) {
		note = fmt.Sprintf("　⚠ 证据链行号 %d 超出该文件总行数 %d（本地源码版本可能不一致）", want, len(lines))
	}
	a.fileLbl.SetText(fmt.Sprintf("%s　·　%d 行　·　编码 %s%s",
		shortPath(path), len(lines), enc, note))
	return true
}

// rebuildHighlights 依据当前上下文为已打开文件重建高亮。
func (a *App) rebuildHighlights() {
	if a.codeView.Path() == "" {
		return
	}
	cur := a.codeView.Path()
	a.codeView.ClearHighlight()
	add := func(loc model.Location, kind HLKind) {
		if loc.StartLine <= 0 {
			return
		}
		p := loc.ResolvedPath
		if p == "" {
			p = a.resolvePath(loc.File)
		}
		if p != cur {
			return
		}
		el, ec := loc.NormalizedEnd()
		a.codeView.AddRange(kind, loc.StartLine, loc.StartCol, el, ec)
	}
	add(a.ctxSink, HLSink)
	for _, s := range a.ctxSource {
		add(s, HLSource)
	}
	a.codeView.Apply()
}

func (a *App) resolvePath(csvPath string) string {
	if a.proj == nil {
		return ""
	}
	r := a.proj.Resolver.Resolve(csvPath)
	if !r.Resolved() {
		return ""
	}
	return r.Path
}

func (a *App) readFile(path string) ([]string, string, bool) {
	if lines, ok := a.fileCache[path]; ok {
		return lines, a.encCache[path], true
	}
	lines, enc, err := sourcefile.ReadLines(path)
	if err != nil {
		a.showError(err)
		return nil, "", false
	}
	a.fileCache[path] = lines
	a.encCache[path] = enc
	return lines, enc, true
}

func (a *App) showError(err error) {
	if err == nil {
		return
	}
	dialog.ShowError(err, a.win)
}

// copyCurrentMessage 复制当前漏洞的中文详情到剪贴板。
func (a *App) copyCurrentMessage() {
	if a.detail.current == nil {
		return
	}
	f := a.detail.current
	text := fmt.Sprintf("%s\n%s\n严重级别：%s\n位置：%s\n\n%s\n\n%s",
		f.RuleDisplayName(), f.RuleName, f.Severity.ZH(), f.Sink.Display(),
		f.DescriptionDisplay(), model.PlainSegments(f.MessageZH))
	a.win.Clipboard().SetContent(text)
	a.statsLbl.SetText("已复制当前漏洞的中文详情到剪贴板")
}

// exportCSV 导出带中文表头的结果文件。
func (a *App) exportCSV() {
	if a.proj == nil {
		dialog.ShowInformation("提示", "请先打开 CodeQL 结果 CSV 文件。", a.win)
		return
	}
	d := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil {
			a.showError(err)
			return
		}
		if wc == nil {
			return
		}
		path := wc.URI().Path()
		_ = wc.Close()
		rows := a.rows
		if len(rows) == 0 {
			rows = a.proj.Findings
		}
		if err := a.proj.ExportZH(path, rows); err != nil {
			a.showError(err)
			return
		}
		dialog.ShowInformation("导出成功",
			fmt.Sprintf("已导出 %d 条记录（带中文表头，UTF-8 BOM）到：\n%s", len(rows), path), a.win)
	}, a.win)
	base := strings.TrimSuffix(filepath.Base(a.proj.CSVPath), filepath.Ext(a.proj.CSVPath))
	d.SetFileName(base + "_中文.csv")
	showFileDialog(d, fyne.NewSize(940, 620))
}
