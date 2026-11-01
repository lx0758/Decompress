# 解压工具构建脚本
#
# 常用命令：
#   make build          编译当前平台二进制到 bin/
#   make test           运行全部测试
#   make check          格式化检查 + vet + 测试
#   make release        交叉编译各平台二进制到 dist/
#   make clean          清理产物
#
# 版本号默认取最近的 git tag（如 v1.0.0），可用 VERSION 覆盖：
#   make release VERSION=1.2.3
#   make release VERSION=$(git describe --tags --always)

BINARY      := decompress
CMD         := ./cmd/decompress
BIN_DIR     := bin
DIST_DIR    := dist

# 版本号：优先用命令行传入的 VERSION，否则取 git tag。
# 有 tag 时形如 v1.0.0，没有 tag 时回退到短 commit hash；
# 不在 git 仓库中则用 dev。
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")

# 注入二进制的版本变量。必须与 cli 包的实际导入路径一致，
# 否则 ldflags 会静默失效，--version 永远显示默认值。
LDFLAGS := -s -w -X github.com/liux/decompress/internal/cli.Version=$(VERSION)

# 交叉编译目标平台
PLATFORMS := \
	windows/amd64 \
	windows/arm64 \
	windows/386 \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	linux/386

.PHONY: all build test vet fmt fmt-check check clean release install demo

all: build

## build 编译当前平台二进制
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)
	@echo "已生成 $(BIN_DIR)/$(BINARY)（版本 $(VERSION)）"

## version 打印当前版本号
version:
	@echo $(VERSION)

## test 运行全部测试
test:
	go test ./... -timeout 120s

## vet 静态检查
vet:
	go vet ./...

## fmt 格式化代码
fmt:
	go fmt ./...

## fmt-check 检查格式是否符合规范
fmt-check:
	@out=$$(gofmt -l . 2>&1); \
	if [ -n "$$out" ]; then \
		echo "以下文件格式不符合规范："; echo "$$out"; exit 1; \
	fi

## check 完整检查：格式 + vet + 测试
check: fmt-check vet test

## release 交叉编译各平台二进制
release:
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="$(DIST_DIR)/$(BINARY)-$(VERSION)-$$os-$$arch$$ext"; \
		echo "编译 $$os/$$arch -> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" $(CMD) || exit 1; \
	done
	@echo "全部平台编译完成（版本 $(VERSION)），产物在 $(DIST_DIR)/"

## install 安装到 GOPATH/bin
install:
	go install $(CMD)

## demo 演示：解压当前目录下的所有压缩文件
demo:
	go run $(CMD) -o ./out .

## clean 清理构建产物
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
	go clean
