// Command codeql-audit 是一个 CodeQL 结果审计辅助工具（桌面版）。
//
// 用法：
//
//	codeql-audit                     # 打开图形界面
//	codeql-audit -csv pyhackme.csv   # 打开界面并直接载入结果文件
//	codeql-audit -cli -csv x.csv -src /path/to/src -out x_中文.csv
//	                                 # 命令行模式：解析 + 中文化 + 导出（便于批量与验证）
package main

import (
	"flag"
	"fmt"
	"os"

	"codeql-audit/internal/config"
	"codeql-audit/internal/project"
	"codeql-audit/internal/translate"
	"codeql-audit/internal/ui"
)

func main() {
	var (
		csvPath     = flag.String("csv", "", "CodeQL 结果 CSV 文件路径")
		srcRoot     = flag.String("src", "", "被审计的源码目录")
		outPath     = flag.String("out", "", "命令行模式下导出的中文 CSV 路径")
		cli         = flag.Bool("cli", false, "使用命令行模式（不启动图形界面）")
		shotPath    = flag.String("shot", "", "用真实渲染器截图到指定 PNG 后退出（用于视觉自检）")
		fontsFlag   = flag.Bool("fonts", false, "打印中文字体诊断报告后退出")
		showVersion = flag.Bool("version", false, "显示版本信息")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("codeql-audit v0.1.0")
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
		fmt.Print(ui.FontDiagnostics(cfg.FontPath))
		return
	}

	if *shotPath != "" {
		if err := ui.Screenshot(cfg, *csvPath, *srcRoot, *shotPath, 0); err != nil {
			fmt.Fprintln(os.Stderr, "截图失败：", err)
			os.Exit(1)
		}
		return
	}

	if *cli {
		if err := runCLI(cfg, *csvPath, *srcRoot, *outPath); err != nil {
			fmt.Fprintln(os.Stderr, "错误：", err)
			os.Exit(1)
		}
		return
	}

	if err := ui.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "启动界面失败：", err)
		os.Exit(1)
	}
}

// runCLI 在命令行下完成解析、中文化与导出，便于批量处理与自动化验证。
func runCLI(cfg *config.Config, csvPath, srcRoot, outPath string) error {
	if csvPath == "" {
		return fmt.Errorf("请用 -csv 指定 CodeQL 结果文件")
	}
	store, err := translate.OpenStore(cfg.TranslateDB)
	if err != nil {
		return err
	}
	defer store.Close()
	engine, err := translate.NewEngine(store, translate.NewLLMClient(translate.LLMConfig{
		Enabled:        cfg.LLM.Enabled,
		BaseURL:        cfg.LLM.BaseURL,
		APIKey:         cfg.LLM.APIKey,
		Model:          cfg.LLM.Model,
		TimeoutSeconds: cfg.LLM.TimeoutSeconds,
		TemplateOnly:   cfg.LLM.TemplateOnly,
	}))
	if err != nil {
		return err
	}

	p, err := project.Load(project.Options{
		CSVPath: csvPath,
		SrcRoot: srcRoot,
		Engine:  engine,
		Progress: func(done, total int, msg string) {
			fmt.Fprintf(os.Stderr, "\r%s", msg)
			if done >= total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if err != nil {
		return err
	}
	s := p.Stats
	fmt.Printf("结果文件　：%s\n", csvPath)
	fmt.Printf("源码目录　：%s\n", orDash(srcRoot))
	fmt.Printf("漏洞总数　：%d\n", s.Total)
	fmt.Printf("规则种类　：%d\n", s.RuleKinds)
	fmt.Printf("涉及文件　：%d\n", s.FileKinds)
	fmt.Printf("已定位源码：%d/%d\n", s.Resolved, s.Total)
	fmt.Printf("待翻译条目：%d\n", s.Pending)
	st := engine.Stats()
	fmt.Printf("翻译命中　：内置词典 %d，翻译库 %d，大模型 %d\n", st.Builtin, st.Store, st.LLM)

	if outPath != "" {
		if err := p.ExportZH(outPath, nil); err != nil {
			return err
		}
		fmt.Printf("已导出中文结果（带中文表头）：%s\n", outPath)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "（未指定）"
	}
	return s
}
