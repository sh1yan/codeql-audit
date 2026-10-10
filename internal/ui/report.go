package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/codeqlcsv"
	"codeql-audit/internal/model"
)

// 本文件实现「导出审计报告 CSV」：把当前界面上的**全部审计信息与结论**
// 汇总成一张宽表（见 internal/codeqlcsv/report.go），用于离线分析。
//
// 与既有两个导出的定位区分：
//   - 顶栏「导出中文 CSV」：中文重排后的 CodeQL 九列，偏交付/回填；
//   - 批量验证里的「导出验证结果 CSV」：只有 AI 判定过的条目，列很少；
//   - 顶栏「导出审计报告」：一行 = 一条漏洞的完整信息，含未做 AI 判定的条目。

// exportReport 弹出审计报告导出对话框（先选范围，再选保存位置）。
func (a *App) exportReport() {
	if a.proj == nil {
		dialog.ShowInformation("提示", "请先打开 CodeQL 结果 CSV 文件。", a.win)
		return
	}
	all := a.proj.Findings
	cur := a.rows

	scope := widget.NewSelect([]string{
		fmt.Sprintf("当前筛选结果（%d 条）", len(cur)),
		fmt.Sprintf("全部记录（%d 条）", len(all)),
	}, nil)
	if len(cur) == 0 {
		scope.SetSelectedIndex(1) // 当前筛选为空时默认导全部，避免导出空表
	} else {
		scope.SetSelectedIndex(0)
	}

	desc := wrapLabel("一行 = 一条漏洞的完整审计信息：原始 CodeQL 九列 + 中文翻译 + 本地路径定位 + " +
		"证据链（沉淀点与全部来源点）+ 已读状态 + AI 误报判定结论。" +
		"带 UTF-8 BOM，Excel 双击不乱码，也可以直接用 pandas 读取做离线分析。")
	desc.Importance = widget.LowImportance

	content := container.NewVBox(
		sectionLabel("导出内容"),
		desc,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel("导出范围"), nil, scope),
	)
	d := dialog.NewCustomConfirm("导出审计报告 CSV", "选择保存位置", "取消", content, func(ok bool) {
		if !ok {
			return
		}
		rows := cur
		if scope.SelectedIndex() == 1 {
			rows = all
		}
		a.saveReport(rows)
	}, a.win)
	d.Resize(fyne.NewSize(660, 340))
	d.Show()
}

// reportRows 把漏洞列表组装成审计报告行（补上界面侧的已读状态）。
func (a *App) reportRows(findings []*model.Finding) []codeqlcsv.ReportRow {
	rows := make([]codeqlcsv.ReportRow, 0, len(findings))
	for _, f := range findings {
		rows = append(rows, codeqlcsv.ReportRow{Finding: f, Reviewed: a.isReviewed(f)})
	}
	return rows
}

// saveReport 把选定的漏洞列表写成审计报告 CSV。
func (a *App) saveReport(findings []*model.Finding) {
	if a.proj == nil {
		return
	}
	rows := a.reportRows(findings)
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
		if err := codeqlcsv.ExportReportFile(path, rows); err != nil {
			a.showError(err)
			return
		}
		ai := 0
		for _, r := range rows {
			if r.Finding != nil && r.Finding.HasAIVerdict() {
				ai++
			}
		}
		dialog.ShowInformation("导出成功",
			fmt.Sprintf("已导出 %d 条漏洞的完整审计信息（其中 %d 条带 AI 判定结论）到：\n%s\n\n"+
				"共 %d 列，含原始行列、本地路径、证据链、已读状态与 AI 判定。",
				len(rows), ai, path, len(codeqlcsv.ReportHeaders)), a.win)
	}, a.win)
	base := strings.TrimSuffix(filepath.Base(a.proj.CSVPath), filepath.Ext(a.proj.CSVPath))
	d.SetFileName(base + "_审计报告.csv")
	d.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
	showFileDialog(d, fyne.NewSize(940, 620))
}
