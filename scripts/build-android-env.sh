#!/usr/bin/env bash
#
# 在 Android 设备（Operit / proot Ubuntu）上构建 HY2 Panel。
#
# 背景：
#   Android 的 /storage（sdcardfs/FUSE）不支持 flock，
#   Go 的模块操作会失败并报：
#       go: RLock .../go.mod: function not implemented
#   因此这里先把源码同步到 Linux 原生路径构建，再拷贝产物回来。
#
# 用法：
#   bash scripts/build-android-env.sh [构建目录，默认 /root/hy2build]

set -u

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${1:-/root/hy2build}"
GO_BIN="${GO_BIN:-/usr/local/go/bin/go}"
OUT_BIN="$SRC_DIR/hy2-panel"

if [ ! -x "$GO_BIN" ]; then
  echo "未找到 Go 可执行文件：$GO_BIN" >&2
  echo "可通过环境变量 GO_BIN 指定，例如：GO_BIN=/usr/bin/go bash scripts/build-android-env.sh" >&2
  exit 1
fi

export HOME="${HOME:-/root}"
export GOPATH="${GOPATH:-/root/go}"
export GOMODCACHE="${GOMODCACHE:-/root/go/pkg/mod}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOSUMDB=off
export PATH="$PATH:$(dirname "$GO_BIN")"

echo "==> 同步源码到原生路径：$BUILD_DIR"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"
cp -r "$SRC_DIR/." "$BUILD_DIR/"

cd "$BUILD_DIR" || exit 1

echo "==> go mod tidy"
"$GO_BIN" mod tidy || exit 1

echo "==> go build"
"$GO_BIN" build -o "$BUILD_DIR/hy2-panel" ./cmd/hy2-panel || exit 1

# 某些环境（proot / 特殊 umask）下 go build 的产物会缺少可执行位，这里显式补上。
chmod +x "$BUILD_DIR/hy2-panel"

echo "==> 回写依赖锁定文件"
cp "$BUILD_DIR/go.mod" "$BUILD_DIR/go.sum" "$SRC_DIR/" 2>/dev/null

echo "==> 拷贝二进制到：$OUT_BIN"
cp "$BUILD_DIR/hy2-panel" "$OUT_BIN"

ls -la "$OUT_BIN"
echo "完成。"