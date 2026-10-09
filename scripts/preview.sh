#!/usr/bin/env bash
#
# 本地预览：启动一个带演示数据的 HY2 Panel 实例，用于查看界面。
#
# 用法：bash scripts/preview.sh [端口，默认 8080]
#
# 说明：
#   - 使用独立的预览数据目录，不影响任何正式部署；
#   - 会自动创建 3 个演示用户，让各页面都有内容；
#   - 由于本机没有运行真正的 Hysteria 2 服务，实时数据区会显示为不可用（属正常）。

set -u

PORT="${1:-8080}"
BIN="${HY2_PANEL_BIN:-/root/hy2build/hy2-panel}"
WEB="${HY2_PANEL_WEB:-/root/hy2build/web/dist}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"
WORK=/tmp/hy2-preview
BASE="http://127.0.0.1:${PORT}"

if [ ! -x "$BIN" ]; then
  echo "找不到可执行的 Panel 二进制：$BIN" >&2
  echo "请先运行 scripts/build-android-env.sh，或通过 HY2_PANEL_BIN 指定。" >&2
  exit 1
fi

if [ ! -f "$WEB/index.html" ]; then
  echo "找不到前端构建产物：$WEB" >&2
  echo "请先构建前端（web/dist），或通过 HY2_PANEL_WEB 指定。" >&2
  exit 1
fi

pkill -f 'hy2-panel -listen' 2>/dev/null
sleep 1
rm -rf "$WORK"
mkdir -p "$WORK/config" "$WORK/data" "$WORK/backup"

cat > "$WORK/config/hysteria.yaml" <<'YAML'
# 预览用官方配置（非真实服务参数）
listen: :443
acme:
  domains:
    - panel.example.com
  email: admin@example.com
  type: http
  http:
    altPort: 80
auth:
  type: userpass
  userpass: {}
trafficStats:
  listen: 127.0.0.1:19999
  secret: preview-secret
quic:
  initStreamReceiveWindow: 8388608
  maxStreamReceiveWindow: 8388608
  initConnReceiveWindow: 20971520
  maxConnReceiveWindow: 20971520
  maxIdleTimeout: 30s
  maxIncomingStreams: 1024
congestion:
  type: bbr
  bbrProfile: standard
outbounds:
  - name: default
    type: direct
YAML

# 用 setsid 脱离当前终端会话，避免脚本退出后预览进程被回收。
if command -v setsid >/dev/null 2>&1; then
  setsid "$BIN" -listen "0.0.0.0:${PORT}" \
    -data-dir "$WORK/data" \
    -hy2-config "$WORK/config/hysteria.yaml" \
    -hy2-bin /nonexistent/hysteria \
    -backup-dir "$WORK/backup" \
    -web-dir "$WEB" \
    -admin-user "$ADMIN_USER" \
    -admin-password "$ADMIN_PASSWORD" \
    > "$WORK/panel.log" 2>&1 &
else
  nohup "$BIN" -listen "0.0.0.0:${PORT}" \
    -data-dir "$WORK/data" \
    -hy2-config "$WORK/config/hysteria.yaml" \
    -hy2-bin /nonexistent/hysteria \
    -backup-dir "$WORK/backup" \
    -web-dir "$WEB" \
    -admin-user "$ADMIN_USER" \
    -admin-password "$ADMIN_PASSWORD" \
    > "$WORK/panel.log" 2>&1 &
fi

PID=$!
sleep 2

if ! kill -0 "$PID" 2>/dev/null; then
  echo "启动失败，日志如下：" >&2
  cat "$WORK/panel.log" >&2
  exit 1
fi

PW="$ADMIN_PASSWORD"

# ---- 登录并写入演示数据 ----
COOKIES="$WORK/cookies.txt"
LOGIN=$(curl -s -c "$COOKIES" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}")
CSRF=$(echo "$LOGIN" | grep -oP '"csrfToken":"\K[^"]+' || true)

create_user() {
  curl -s -o /dev/null -b "$COOKIES" -H "X-CSRF-Token: $CSRF" \
    -X POST "$BASE/api/users" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$2\",\"note\":\"$3\"}"
}

create_user "alice"   "alice-2026"   "家庭成员 · 手机"
create_user "bob"     "bob-2026"     "笔记本电脑"
create_user "charlie" "charlie-2026" "旅行备用"

# 写入一些设置，让「系统」页有内容
curl -s -o /dev/null -b "$COOKIES" -H "X-CSRF-Token: $CSRF" \
  -X PUT "$BASE/api/settings" -H 'Content-Type: application/json' \
  -d '{"serverHost":"panel.example.com","subBaseURL":"","mixedPort":7890,"logHysteria":"journal:hysteria-server","logPanel":"","logNginx":"/var/log/nginx/error.log"}'

{
  echo "PID=$PID"
  echo "URL=$BASE"
  echo "USER=admin"
  echo "PASSWORD=$PW"
} > "$WORK/access.txt"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " HY2 Panel 本地预览已启动"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " 地址    ${BASE}"
echo " 用户名  admin"
echo " 密码    ${PW}"
echo
echo " 进程 PID ${PID}（停止：kill ${PID}）"
echo " 访问信息也已写入 ${WORK}/access.txt"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"