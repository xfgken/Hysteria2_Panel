#!/usr/bin/env bash
#
# HY2 Panel 总验证：一次跑完所有可自动化验证项。
#
# 用法：bash scripts/verify-all.sh [构建目录，默认 /root/hy2build]

set -u

BUILD_DIR="${1:-/root/hy2build}"
GO_BIN=/usr/local/go/bin/go
SRC=/storage/emulated/0/AppProjects/HY2Panel

export PATH="$PATH:/usr/local/go/bin"
export HOME=/root
export GOPATH=/root/go
export GOMODCACHE=/root/go/pkg/mod
export GOFLAGS=-mod=mod

pass=0
fail=0
section() { printf '\n\033[36m===== %s =====\033[0m\n' "$1"; }
ok()   { printf '\033[32m  ✓\033[0m %s\n' "$1"; pass=$((pass + 1)); }
no()   { printf '\033[31m  ✗\033[0m %s\n' "$1"; fail=$((fail + 1)); }

# ---------------------------------------------------------------------------
section "1. 后端静态检查（go vet）"
if (cd "$BUILD_DIR" && "$GO_BIN" vet ./... >/tmp/vet.log 2>&1); then
  ok "go vet 零告警"
else
  no "go vet 有问题"; cat /tmp/vet.log
fi

# ---------------------------------------------------------------------------
section "2. 后端单元测试与集成测试（go test）"
if (cd "$BUILD_DIR" && "$GO_BIN" test -count=1 ./... >/tmp/test.log 2>&1); then
  while read -r line; do printf '    %s\n' "$line"; done < <(grep -E '^(ok|FAIL|---)' /tmp/test.log)
  ok "全部测试通过"
else
  no "测试未通过"; cat /tmp/test.log
fi

# ---------------------------------------------------------------------------
section "3. CLI 子命令"
BIN="$BUILD_DIR/hy2-panel"
if "$BIN" help >/tmp/cli-help.log 2>&1 && grep -q "uninstall" /tmp/cli-help.log; then
  ok "help 输出正常"
else
  no "help 输出异常"
fi

WORK=/tmp/hy2cli
rm -rf "$WORK"; mkdir -p "$WORK/backup"
printf 'listen: :443\nauth:\n  type: password\n  password: x\n' > "$WORK/hysteria.yaml"
if "$BIN" backup -hy2-config "$WORK/hysteria.yaml" -backup-dir "$WORK/backup" >/tmp/cli-backup.log 2>&1; then
  ok "backup 成功：$(ls "$WORK/backup" | head -1)"
else
  no "backup 失败"; cat /tmp/cli-backup.log
fi

printf 'listen: :9999\nauth:\n  type: password\n  password: y\n' > "$WORK/hysteria.yaml"
if "$BIN" restore -hy2-config "$WORK/hysteria.yaml" -backup-dir "$WORK/backup" >/tmp/cli-restore.log 2>&1; then
  if grep -q ":443" "$WORK/hysteria.yaml"; then
    ok "restore 成功（内容已还原）"
  else
    no "restore 后内容未还原"
  fi
else
  no "restore 失败"; cat /tmp/cli-restore.log
fi

# ---------------------------------------------------------------------------
section "4. 接口冒烟测试"
if bash "$SRC/scripts/smoke-test.sh" "$BIN" >/tmp/smoke.log 2>&1; then
  if grep -q "冒烟测试结束" /tmp/smoke.log; then
    ok "冒烟测试全流程通过"
    grep -E 'HTTP (401|403)' /tmp/smoke.log | while read -r l; do printf '    %s\n' "$l"; done
  else
    no "冒烟测试未跑完"
  fi
else
  no "冒烟测试脚本失败"
fi

# ---------------------------------------------------------------------------
section "5. 前后端联调测试"
if bash "$SRC/scripts/e2e-web-test.sh" "$BIN" "$BUILD_DIR/web/dist" >/tmp/e2e.log 2>&1; then
  grep -E 'HTTP [0-9]{3}' /tmp/e2e.log | while read -r l; do printf '    %s\n' "$l"; done
  if grep -q "HTTP 200" /tmp/e2e.log; then
    ok "静态资源与接口联调通过"
  else
    no "联调未返回 200"
  fi
else
  no "联调测试脚本失败"
fi

# ---------------------------------------------------------------------------
printf '\n\033[36m===== 汇总 =====\033[0m\n'
printf '  通过 %d 项，失败 %d 项\n' "$pass" "$fail"
pkill -f 'hy2-panel -listen' 2>/dev/null
[ "$fail" -eq 0 ] && exit 0 || exit 1