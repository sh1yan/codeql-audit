// Command codeql-audit 是 CodeQL 结果审计辅助工具（Wails 桌面版）。
//
// 用法：
//
//	codeql-audit                     # 打开图形界面
//	codeql-audit -csv pyhackme.csv   # 打开界面并直接载入结果文件
//	codeql-audit -cli -csv x.csv -src /path/to/src -out x_中文.csv
//	                                 # 命令行模式：解析 + 中文化 + 导出
package main

import (
	"embed"
	"flag"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"codeql-audit/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	var (
		csvPath     = flag.String("csv", "", "CodeQL 结果 CSV 文件路径")
		srcRoot     = flag.String("src", "", "被审计的源码目录")
		outPath     = flag.String("out", "", "命令行模式下导出的中文 CSV 路径")
		cli         = flag.Bool("cli", false, "使用命令行模式（不启动图形界面）")
		fontsFlag   = flag.Bool("fonts", false, "打印 WebView 字体说明（Wails 版由系统渲染中文字体）")
		showVersion = flag.Bool("version", false, "显示版本信息")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("codeql-audit v%s (Wails)\n", appVersion)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取配置失败：", err)
		os.Exit(1)
	}
	if *csvPath != "" {
		cfg.LastProject = *csvPath
		st := cfg.ProjectFor(*csvPath, *srcRoot)
		if *srcRoot != "" {
			st.SrcRoot = *srcRoot
		}
		_ = cfg.Save()
	}

	if *fontsFlag {
		fmt.Println("Wails 版界面使用系统 WebView 渲染，中文字体由操作系统负责（macOS 的 PingFang SC、Windows 的微软雅黑等）。")
		fmt.Println("如仍有字体问题，可在 frontend/src/index.css 里调整 --font-sans / --font-mono。")
		return
	}

	if *cli {
		if err := runCLI(cfg, *csvPath, *srcRoot, *outPath); err != nil {
			fmt.Fprintln(os.Stderr, "错误：", err)
			os.Exit(1)
		}
		return
	}

	app := NewApp(cfg, *csvPath, *srcRoot)

	width, height := int(cfg.WindowWidth), int(cfg.WindowHeight)
	if width < 1100 {
		width = 1480
	}
	if height < 720 {
		height = 930
	}

	err = wails.Run(&options.App{
		Title:     "CodeQL 漏洞审计",
		Width:     width,
		Height:    height,
		MinWidth:  1120,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 11, G: 15, B: 25, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "CodeQL 漏洞审计",
				Message: "v" + appVersion + "\n把 CodeQL CSV 变成可点、可跳、可读的审计界面。",
				Icon:    appIcon,
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动界面失败：", err)
		os.Exit(1)
	}
}
