package ui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"codeql-audit/internal/fontutil"
)

// FontInfo 记录本次运行实际使用的中文字体。
type FontInfo struct {
	Regular    string
	Bold       string
	Mono       string
	FromTTC    bool
	Missing    bool
	Candidates int
}

// Report 返回可直接显示在界面上的字体说明。
func (f FontInfo) Report() string {
	if f.Missing {
		return "⚠ 未找到可用中文字体，中文可能显示为方块。请在「设置 → 界面字体」中手工指定字体文件。"
	}
	parts := []string{}
	if f.Regular != "" {
		parts = append(parts, "正文 "+filepath.Base(f.Regular))
	}
	if f.Mono != "" && f.Mono != f.Regular {
		parts = append(parts, "代码 "+filepath.Base(f.Mono))
	}
	if f.Bold != "" {
		parts = append(parts, "粗体 "+filepath.Base(f.Bold))
	}
	return "界面字体：" + strings.Join(parts, "　·　")
}

// cjkTheme 在 Fyne 默认主题之上注入中文字体与高对比度配色。
//
// Fyne 只允许每种文字样式返回一个字体资源，且其内置字体不含中日韩字形，
// 系统回退又只按系统语言挑选（英文 locale 下不会选中文字体），
// 因此这里显式接管字体选择。
type cjkTheme struct {
	base    fyne.Theme
	regular fyne.Resource
	bold    fyne.Resource
	mono    fyne.Resource
}

func (t *cjkTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	// 先查高对比度覆盖表（见 palette.go），未覆盖的仍用系统主题，
	// 这样既解决了「灰字费眼」，又不会把其它颜色改坏。
	if c, ok := readableColor(n, v); ok {
		return c
	}
	return t.base.Color(n, v)
}

func (t *cjkTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.base.Icon(n) }

func (t *cjkTheme) Size(n fyne.ThemeSizeName) float32 { return t.base.Size(n) }

func (t *cjkTheme) Font(s fyne.TextStyle) fyne.Resource {
	if s.Monospace {
		if t.mono != nil {
			return t.mono
		}
		if t.regular != nil {
			return t.regular
		}
		return t.base.Font(s)
	}
	if s.Bold {
		if t.bold != nil {
			return t.bold
		}
		if t.regular != nil {
			return t.regular
		}
		return t.base.Font(s)
	}
	if t.regular != nil {
		return t.regular
	}
	return t.base.Font(s)
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func homeFont(name string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, "Library", "Fonts", name)
}

// regularFontCandidates 返回正文中文字体候选，按优先级排列。
func regularFontCandidates() []string {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	winFonts := filepath.Join(win, "Fonts")
	mac := []string{
		"/System/Library/Fonts/PingFang.ttc", // macOS 默认中文界面字体
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/STHeiti Light.ttc",
		"/System/Library/Fonts/Supplemental/Songti.ttc",
		"/Library/Fonts/Arial Unicode.ttf",
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		"/System/Library/Fonts/AppleSDGothicNeo.ttc",
		homeFont("Arial Unicode.ttf"),
		homeFont("msyh.ttf"),
		homeFont("SimHei.ttf"),
	}
	windows := []string{
		filepath.Join(winFonts, "msyh.ttc"), // 微软雅黑
		filepath.Join(winFonts, "msyh.ttf"),
		filepath.Join(winFonts, "Deng.ttf"), // 等线
		filepath.Join(winFonts, "simhei.ttf"),
		filepath.Join(winFonts, "simsun.ttc"),
		filepath.Join(winFonts, "msjh.ttc"),
	}
	linux := []string{
		"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/truetype/arphic/uming.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansSC-Regular.otf",
	}
	switch runtime.GOOS {
	case "darwin":
		return append(mac, append(windows, linux...)...)
	case "windows":
		return append(windows, append(mac, linux...)...)
	default:
		return append(linux, append(mac, windows...)...)
	}
}

// boldFontCandidates 返回粗体中文字体候选。
func boldFontCandidates() []string {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	winFonts := filepath.Join(win, "Fonts")
	return []string{
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/PingFang.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		filepath.Join(winFonts, "msyhbd.ttc"),
		filepath.Join(winFonts, "simhei.ttf"),
		"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Bold.otf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
		homeFont("msyhbd.ttf"),
	}
}

// monoFontCandidates 返回「等宽中文」字体候选。
// 这类字体能同时保证代码对齐与中文注释可读，属于最优解。
func monoFontCandidates() []string {
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	winFonts := filepath.Join(win, "Fonts")
	var out []string
	for _, n := range []string{
		"SarasaMonoSC-Regular.ttf", "Sarasa-Mono-SC-Regular.ttf",
		"sarasa-mono-sc-regular.ttf", "SarasaMonoSC.ttf",
	} {
		out = append(out, homeFont(n), filepath.Join("/Library/Fonts", n), filepath.Join(winFonts, n))
	}
	return append(out,
		"/usr/share/fonts/truetype/sarasa/Sarasa-Mono-SC-Regular.ttf",
		"/usr/share/fonts/opentype/noto/NotoSansMonoCJKsc-Regular.otf",
		"/usr/share/fonts/opentype/noto/NotoSansMonoCJK-Regular.ttc",
	)
}

// loadFontResource 读取字体文件并抽取可用字重。
func loadFontResource(path string) (fyne.Resource, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	usable, chk := fontutil.CheckFile(data)
	if !chk.OK {
		return nil, false, fmt.Errorf("%s 不含中文字形", filepath.Base(path))
	}
	isCollection := fontutil.IsCollection(data)
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return fyne.NewStaticResource(name, usable), isCollection, nil
}

// pickFont 按候选顺序找出第一个可用的中文字体。
func pickFont(candidates []string, tried *int) (fyne.Resource, string, bool) {
	for _, p := range candidates {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		*tried++
		if res, isTTC, err := loadFontResource(p); err == nil {
			return res, p, isTTC
		}
	}
	return nil, "", false
}

// applyCJKTheme 挑选中文字体并注入主题。custom 为用户在设置里指定的字体文件（可为空）。
func applyCJKTheme(a fyne.App, custom string) FontInfo {
	info := FontInfo{}
	var tried int

	if strings.TrimSpace(custom) != "" {
		if res, isTTC, err := loadFontResource(custom); err == nil {
			info.Regular, info.FromTTC = custom, isTTC
			info.Candidates = tried
			a.Settings().SetTheme(&cjkTheme{base: theme.DefaultTheme(), regular: res, bold: res, mono: res})
			return info
		}
	}

	regularRes, regularPath, regTTC := pickFont(regularFontCandidates(), &tried)
	info.Candidates = tried
	if regularRes == nil {
		info.Missing = true
		return info
	}
	info.Regular, info.FromTTC = regularPath, regTTC

	monoRes, monoPath, _ := pickFont(monoFontCandidates(), &tried)
	info.Mono = monoPath
	// 没有等宽中文字体时，代码视图也使用中文字体：
	// 牺牲一点等宽对齐，换取中文注释不再显示为方块。
	if monoRes == nil {
		monoRes = regularRes
		info.Mono = regularPath
	}

	boldRes, boldPath, _ := pickFont(boldFontCandidates(), &tried)
	info.Bold = boldPath
	if boldRes == nil {
		boldRes = regularRes
		info.Bold = regularPath
	}

	a.Settings().SetTheme(&cjkTheme{
		base:    theme.DefaultTheme(),
		regular: regularRes,
		bold:    boldRes,
		mono:    monoRes,
	})
	return info
}

// FontDiagnostics 生成一份中文字体诊断报告，用于排查「中文显示为方块」的问题。
func FontDiagnostics(custom string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "操作系统：%s/%s\n", runtime.GOOS, runtime.GOARCH)
	if custom != "" {
		fmt.Fprintf(&b, "用户指定字体：%s\n", custom)
	}
	check := func(title string, paths []string) {
		fmt.Fprintf(&b, "\n候选%s：\n", title)
		for _, p := range paths {
			st, err := os.Stat(p)
			if err != nil || st.IsDir() {
				continue
			}
			res, isTTC, err := loadFontResource(p)
			if err != nil {
				fmt.Fprintf(&b, "  [不可用] %s（%v）\n", p, err)
				continue
			}
			kind := "单字体文件"
			if isTTC {
				kind = "字体集合(已抽取字重)"
			}
			_ = res
			fmt.Fprintf(&b, "  [可用]   %s　%s\n", p, kind)
		}
	}
	check("正文", regularFontCandidates())
	check("粗体", boldFontCandidates())
	check("等宽中文", monoFontCandidates())

	info := pickCJKInfo(custom)
	fmt.Fprintf(&b, "\n实际选用：\n  %s\n", info.Report())
	if info.Missing {
		fmt.Fprintln(&b, "\n建议：在软件「设置 → 界面字体」中选择一个 .ttf/.otf/.ttc 中文字体文件。")
	}
	return b.String()
}

// pickCJKInfo 只做探测，不修改应用主题。
func pickCJKInfo(custom string) FontInfo {
	info := FontInfo{}
	var tried int
	if strings.TrimSpace(custom) != "" {
		if _, isTTC, err := loadFontResource(custom); err == nil {
			info.Regular, info.FromTTC, info.Mono, info.Bold = custom, isTTC, custom, custom
			return info
		}
	}
	reg, regPath, regTTC := pickFont(regularFontCandidates(), &tried)
	info.Candidates = tried
	if reg == nil {
		info.Missing = true
		return info
	}
	info.Regular, info.FromTTC = regPath, regTTC
	if _, monoPath, _ := pickFont(monoFontCandidates(), &tried); monoPath != "" {
		info.Mono = monoPath
	} else {
		info.Mono = regPath
	}
	if _, boldPath, _ := pickFont(boldFontCandidates(), &tried); boldPath != "" {
		info.Bold = boldPath
	} else {
		info.Bold = regPath
	}
	return info
}
