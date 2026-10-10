# CodeQL 漏洞审计工具 —— 构建与打包
#
# 常用命令：
#   make build          编译到 build/codeql-audit
#   make run            编译并运行（需先设置 CSV / SRC）
#   make test           跑全部单元测试
#   make package-mac    打包成 macOS .app（需 fyne CLI）
#   make package-win    打包成 Windows .exe（建议在 Windows 上执行）
#   make package-linux  打包成 Linux 可执行文件（建议在 Linux 上执行）
#   make fonts          打印中文字体诊断报告
#
# fyne CLI 安装：go install fyne.io/tools/cmd/fyne@latest

APP      := codeql-audit
BIN_DIR  := build
FYNE     ?= fyne
ICON     := assets/icon.png
CSV      ?= ../pyhackme.csv
SRC      ?=

.PHONY: help build run test vet fmt tidy clean package-mac package-win package-linux fonts cross

help:
	@echo "可用目标："
	@echo "  build           编译当前平台可执行文件到 $(BIN_DIR)/$(APP)"
	@echo "  run             运行（make run CSV=../pyhackme.csv SRC=/path/to/src）"
	@echo "  test            运行全部测试"
	@echo "  vet / fmt/tidy  代码检查与格式化"
	@echo "  fonts           打印中文字体诊断"
	@echo "  package-mac     macOS .app"
	@echo "  package-win     Windows .exe"
	@echo "  package-linux   Linux 可执行文件"
	@echo "  cross           用 fyne-cross(Docker) 交叉打包 Windows/Linux"

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(APP) .

run:
	go run . -csv "$(CSV)" -src "$(SRC)"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

fonts:
	go run . -fonts

clean:
	rm -rf $(BIN_DIR) $(APP).app $(APP).exe

package-mac:
	$(FYNE) package -os darwin -icon $(ICON)

package-win:
	$(FYNE) package -os windows -icon $(ICON)

package-linux:
	$(FYNE) package -os linux -icon $(ICON)

# 在 macOS/Linux 上用 Docker 交叉打包（需先安装 fyne-cross）
cross:
	fyne-cross windows -icon $(ICON)
	fyne-cross linux -icon $(ICON)
