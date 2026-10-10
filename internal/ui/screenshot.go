package ui

import (
	"fmt"
	"image/png"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"codeql-audit/internal/config"
)

// Screenshot 使用**真实渲染器**（含系统字体回退）把界面渲染成 PNG，然后退出。
//
// 这条路径与 go test 下的 test 驱动不同：测试驱动会关闭系统字体加载，
// 中文会显示为方块，因此视觉自检必须走真实驱动。
func Screenshot(cfg *config.Config, csvPath, srcRoot, outPath string, delay time.Duration) error {
	if delay <= 0 {
		delay = 1500 * time.Millisecond
	}
	a, err := newApp(app.NewWithID("com.codeqlaudit.shot"), cfg)
	if err != nil {
		return err
	}
	if csvPath != "" {
		if err := a.doLoad(csvPath, srcRoot, nil); err != nil {
			return err
		}
		a.applyProject(false)
		// 选中一条信息最丰富的记录
		for i, f := range a.rows {
			if f.RuleName == "SQL query built into user-controlled sources" ||
				f.RuleName == "SQL query built from user-controlled sources" {
				a.list.Select(i)
				break
			}
		}
	}
	go func() {
		time.Sleep(delay)
		fyne.Do(func() {
			img := a.win.Canvas().Capture()
			f, err := os.Create(outPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, "写入截图失败：", err)
			} else {
				if encErr := png.Encode(f, img); encErr != nil {
					fmt.Fprintln(os.Stderr, "编码截图失败：", encErr)
				}
				_ = f.Close()
				fmt.Printf("已保存截图：%s (%v)\n", outPath, img.Bounds().Size())
			}
			a.fyneApp.Quit()
		})
	}()
	a.win.ShowAndRun()
	return nil
}
