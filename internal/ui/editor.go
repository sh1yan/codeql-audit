package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"fyne.io/fyne/v2/dialog"

	"codeql-audit/internal/config"
	"codeql-audit/internal/model"
)

// ------------------------- 用编辑器打开 -------------------------

// defaultEditorArgs 是 VS Code 家族的默认参数模板。
const defaultEditorArgs = "-g {file}:{line}:{col}"

// editorCandidates 返回各平台自动探测编辑器的候选路径/命令（按优先级）。
func editorCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			"/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code",
			"/Applications/Cursor.app/Contents/Resources/app/bin/cursor",
			"/opt/homebrew/bin/code", "/usr/local/bin/code",
			"/Applications/Sublime Text.app/Contents/SharedSupport/bin/subl",
			"code", "cursor", "subl",
		}
	case "windows":
		return []string{"code.cmd", "code", "cursor.cmd", "cursor"}
	default:
		return []string{"/usr/bin/code", "/usr/local/bin/code", "/snap/bin/code", "code", "cursor", "subl"}
	}
}

// detectEditor 自动探测一个可用的编辑器；找不到时返回空串（交由调用方提示用户去设置）。
func detectEditor() string {
	for _, c := range editorCandidates() {
		if strings.ContainsAny(c, "/\\") {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// renderEditorArgs 把参数模板里的 {file} {line} {col} 换成实际值。
func renderEditorArgs(tmpl, file string, line, col int) []string {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = defaultEditorArgs
	}
	if line <= 0 {
		line = 1
	}
	repl := strings.NewReplacer(
		"{file}", file,
		"{path}", file,
		"{line}", fmt.Sprintf("%d", line),
		"{col}", fmt.Sprintf("%d", col),
	)
	rendered := repl.Replace(tmpl)
	out := make([]string, 0, 4)
	for _, part := range strings.Fields(rendered) {
		out = append(out, part)
	}
	return out
}

// editorCommand 组装「用编辑器打开」要执行的命令。
//
// 支持三种配置：
//  1. 可执行文件路径/命令名（例如 /usr/local/bin/code）→ 直接执行 + 参数模板；
//  2. macOS 的 .app 包路径 → 走 open -a，用 --args 传参数；
//  3. 留空 → 自动探测常见编辑器。
func editorCommand(cfg config.EditorSettings, file string, line, col int) (*exec.Cmd, string, error) {
	cmdPath := strings.TrimSpace(cfg.Cmd)
	if cmdPath == "" {
		cmdPath = detectEditor()
	}
	if cmdPath == "" {
		return nil, "", fmt.Errorf("没有找到可用的编辑器。请在「设置 → 用编辑器打开」里填写编辑器的可执行文件路径")
	}
	isApp := strings.HasSuffix(strings.ToLower(cmdPath), ".app")
	if strings.ContainsAny(cmdPath, "/\\") {
		// 用户手工填的路径要尽早校验，否则错误会拖到 Start() 才暴露
		st, err := os.Stat(cmdPath)
		if err != nil || (!isApp && st.IsDir()) {
			return nil, "", fmt.Errorf("配置的编辑器不存在或不是可执行文件：%s\n请在「设置 → 用编辑器打开」里重新选择", cmdPath)
		}
	}
	args := renderEditorArgs(cfg.Args, file, line, col)

	if isApp {
		openArgs := []string{"-a", cmdPath}
		if len(args) > 0 {
			openArgs = append(openArgs, "--args")
			openArgs = append(openArgs, args...)
		}
		return exec.Command("open", openArgs...), "open -a " + cmdPath, nil
	}
	if runtime.GOOS == "windows" && !strings.ContainsAny(cmdPath, "/\\") {
		return exec.Command("cmd", append([]string{"/c", cmdPath}, args...)...), cmdPath, nil
	}
	return exec.Command(cmdPath, args...), cmdPath + " " + strings.Join(args, " "), nil
}

func (a *App) openInEditor() {
	path := a.codeView.Path()
	if path == "" {
		dialog.ShowInformation("提示", "还没有打开任何文件。请先在左侧选择一条漏洞。", a.win)
		return
	}
	line := a.curLine
	if line <= 0 {
		line = 1
	}
	col := 1
	if a.ctxSink.StartCol > 0 && samePath(a.ctxSink, path) {
		col = a.ctxSink.StartCol
	}
	cmd, desc, err := editorCommand(a.cfg.Editor, path, line, col)
	if err != nil {
		dialog.ShowInformation("打开失败", err.Error()+"\n\n可在「设置 → 用编辑器打开」里指定编辑器路径与参数。", a.win)
		return
	}
	if err := cmd.Start(); err != nil {
		dialog.ShowInformation("打开失败", fmt.Sprintf(
			"未能启动编辑器：%s\n命令：%s\n\n文件位置：\n%s:%d:%d\n\n"+
				"请在「设置 → 用编辑器打开」里填写编辑器可执行文件的完整路径。",
			err, desc, path, line, col), a.win)
		return
	}
	a.statsLbl.SetText("已用编辑器打开：" + desc)
}

// samePath 判断一个位置是否指向给定文件。
func samePath(loc model.Location, path string) bool {
	p := loc.ResolvedPath
	if p == "" {
		p = loc.File
	}
	return p == path
}
