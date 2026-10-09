# HY2 Panel —— 构建与开发入口
#
# 说明：
#   本 Makefile 假设构建发生在 Linux 原生文件系统上。
#   若在 Android 的 /storage（sdcardfs）上，请改用 scripts/build-android-env.sh，
#   因为该文件系统不支持 Go 需要的文件锁。

GO      ?= go
BINARY  ?= hy2-panel
PKG     ?= ./cmd/hy2-panel
VERSION ?= 0.1.0-dev

.PHONY: all build vet test tidy clean run

all: build

## 编译主程序
build:
	$(GO) build -ldflags "-X github.com/hy2-panel/hy2-panel/internal/server.Version=$(VERSION)" \
		-o $(BINARY) $(PKG)

## 静态检查
vet:
	$(GO) vet ./...

## 单元测试
test:
	$(GO) test ./...

## 整理依赖
tidy:
	$(GO) mod tidy

## 本地运行（开发用）
run: build
	./$(BINARY) -listen :8080 -data-dir ./data -hy2-config ./config/hysteria.yaml

## 清理构建产物
clean:
	rm -f $(BINARY)
	rm -rf dist
