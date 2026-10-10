package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"codeql-audit/internal/config"
	"codeql-audit/internal/model"
	"codeql-audit/internal/verify"
)

// 本文件集中处理「跨会话要记住的东西」：按项目持久化的已读标记、AI 判定结论，
// 以及配置落盘的防抖。界面其它部分只调用这里的几个方法。

// projectState 返回当前项目的持久化状态（已读、AI 判定都存这里）。
func (a *App) projectState() *config.ProjectState {
	if a.cfg == nil {
		return nil
	}
	csv := a.cfg.LastProject
	root := ""
	if a.proj != nil {
		csv = a.proj.CSVPath
		root = a.proj.SrcRoot
	}
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	return a.cfg.ProjectFor(csv, root)
}

// flushProjectState 把内存里的已读状态与 AI 判定写回项目状态。
func (a *App) flushProjectState() {
	st := a.projectState()
	if st == nil {
		return
	}
	if len(a.reviewed) > 0 {
		if st.Reviewed == nil {
			st.Reviewed = map[string]bool{}
		}
		for k, v := range a.reviewed {
			if v {
				st.Reviewed[k] = true
			}
		}
	}
	if len(a.verdicts) > 0 {
		if st.Verdicts == nil {
			st.Verdicts = map[string]config.VerdictRecord{}
		}
		for k, v := range a.verdicts {
			st.Verdicts[k] = v
		}
	}
}

// loadProjectStateFrom 从配置里恢复某个项目的已读状态与 AI 判定。
func (a *App) loadProjectStateFrom(csvPath string) {
	a.reviewed = map[string]bool{}
	a.verdicts = map[string]config.VerdictRecord{}
	st := a.cfg.ProjectFor(csvPath, "")
	if st == nil {
		return
	}
	for k, v := range st.Reviewed {
		if v {
			a.reviewed[k] = true
		}
	}
	for k, v := range st.Verdicts {
		a.verdicts[k] = v
	}
}

// scheduleSave 延迟落盘，避免「点一条漏洞写一次配置」。
func (a *App) scheduleSave() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(1200*time.Millisecond, func() {
		a.saveMu.Lock()
		a.saveTimer = nil
		a.saveMu.Unlock()
		fyne.Do(func() { a.saveNow() })
	})
}

// saveNow 立即落盘（主线程调用，避免并发写同一份 JSON）。
//
// 注意：这里绝不能碰任何控件。它是防抖定时器触发的，可能在窗口已销毁之后
// 才执行，刷新已释放的 Label 会让 Fyne 直接 panic（实测崩在 richtext 里）。
func (a *App) saveNow() {
	a.saveMu.Lock()
	closing := a.closing
	a.saveMu.Unlock()
	if closing {
		return
	}
	a.flushProjectState()
	if err := a.cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "配置保存失败：", err)
	}
}

// stopSaveTimer 取消待执行的防抖落盘。
func (a *App) stopSaveTimer() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.closing = true
	if a.saveTimer != nil {
		a.saveTimer.Stop()
		a.saveTimer = nil
	}
}

// verdictCounts 统计三类 AI 判定结果。
func (a *App) verdictCounts() (falsePos, confirmed, unsure int) {
	if a.proj == nil {
		return
	}
	for _, f := range a.proj.Findings {
		switch f.AIVerdict {
		case string(verify.FalsePositive):
			falsePos++
		case string(verify.TruePositive):
			confirmed++
		case string(verify.Uncertain):
			unsure++
		}
	}
	return
}

// isReviewed 判断一条漏洞是否已读。
func (a *App) isReviewed(f *model.Finding) bool {
	if f == nil {
		return false
	}
	return a.reviewed[f.Key()]
}

// markReviewed 把一条漏洞标记为已读（并刷新对应列表行的样式）。
func (a *App) markReviewed(f *model.Finding) {
	if f == nil {
		return
	}
	key := f.Key()
	if key == "" || a.reviewed[key] {
		return
	}
	a.reviewed[key] = true
	if i := a.rowIndex(f); i >= 0 {
		a.list.RefreshItem(widget.ListItemID(i))
	}
	a.updateCountsOnly()
	a.updateStats()
	a.scheduleSave()
}

// updateCountsOnly 只刷新计数文案，不重新过滤列表（避免点一条就重建列表）。
func (a *App) updateCountsOnly() {
	if a.proj == nil {
		return
	}
	unread := 0
	for _, f := range a.proj.Findings {
		if !a.isReviewed(f) {
			unread++
		}
	}
	falsePos, confirmed, unsure := a.verdictCounts()
	a.countLbl.SetText(fmt.Sprintf("显示 %d / %d 条　·　未读 %d　·　AI：误报 %d / 确认 %d / 存疑 %d",
		len(a.rows), a.proj.Stats.Total, unread, falsePos, confirmed, unsure))
}

// rowIndex 返回一条漏洞在当前列表中的下标（-1 表示不在列表里）。
func (a *App) rowIndex(f *model.Finding) int {
	for i, r := range a.rows {
		if r == f {
			return i
		}
	}
	return -1
}

// markAllReviewed 把当前列表里的条目全部标为已读。
func (a *App) markAllReviewed() {
	if a.proj == nil {
		return
	}
	targets := a.rows
	if len(targets) == 0 {
		targets = a.proj.Findings
	}
	for _, f := range targets {
		a.reviewed[f.Key()] = true
	}
	a.refreshList()
	a.deleteMissingReviewed()
	a.saveNow()
	a.statsLbl.SetText(fmt.Sprintf("已把 %d 条标记为已读", len(targets)))
}

// resetReviewed 清空当前项目的全部已读标记。
func (a *App) resetReviewed() {
	a.reviewed = map[string]bool{}
	a.refreshList()
	a.saveNow()
	a.statsLbl.SetText("已重置已读标记")
}

// deleteMissingReviewed 清理列表中已不存在的已读键（CSV 变更后避免配置无限膨胀）。
func (a *App) deleteMissingReviewed() {
	if a.proj == nil {
		return
	}
	alive := map[string]bool{}
	for _, f := range a.proj.Findings {
		alive[f.Key()] = true
	}
	for k := range a.reviewed {
		if !alive[k] {
			delete(a.reviewed, k)
		}
	}
	st := a.projectState()
	if st != nil && st.Reviewed != nil {
		for k := range st.Reviewed {
			if !alive[k] {
				delete(st.Reviewed, k)
			}
		}
	}
}
