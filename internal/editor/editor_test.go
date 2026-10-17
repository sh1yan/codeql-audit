package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"codeql-audit/internal/config"
)

func TestRenderArgs(t *testing.T) {
	got := RenderArgs("", "/tmp/a.py", 12, 7)
	want := []string{"-g", "/tmp/a.py:12:7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RenderArgs = %#v, want %#v", got, want)
	}
	got = RenderArgs("--line {line} --file {file}", "/tmp/a.py", 0, 0)
	want = []string{"--line", "1", "--file", "/tmp/a.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RenderArgs = %#v, want %#v", got, want)
	}
}

func TestCommandMissingEditor(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, _, err := Command(config.EditorSettings{Cmd: missing}, "/tmp/a.py", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("expected missing editor error, got %v", err)
	}
}

func TestCommandExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style executable")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "fake-editor")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, desc, err := Command(config.EditorSettings{Cmd: exe, Args: "-g {file}:{line}"}, "/tmp/a.py", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || !strings.Contains(desc, "/tmp/a.py:3") {
		t.Fatalf("cmd=%v desc=%q", cmd, desc)
	}
}

func TestRevealEmptyPath(t *testing.T) {
	if err := Reveal(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}
