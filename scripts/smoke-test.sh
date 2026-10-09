#!/usr/bin/env bash
#
# HY2 Panel 端到端冒烟测试
#
# 覆盖：登录 / 会话 / CSRF、用户 CRUD、HY2 URI、订阅（Clash + URI）、
#       配置读取 / 校验 / 应用 / 备份、设置、系统信息、日志、鉴权边界。
#
# 用法：bash scripts/smoke-test.sh [hy2-panel 二进制路径]

set -u

BIN="${1:-/root/hy2build/hy2-panel}"
WORK=/tmp/hy2smoke
BASE=http://127.0.0.1:18099

rm -rf "$WORK"
mkdir -p "$WORK/config" "$WORK/data" "$WORK/backup"

cat > "$WORK/config/hysteria.yaml" <<'YAML'
listen: :443
auth:
  type: userpass
  userpass:
    demo: demopass
trafficStats:
  listen: 127.0.0.1:19999
  secret: testsecret
quic:
  maxIdleTimeout: 30s
YAML

"$BIN" -listen 127.0.0.1:18099 \
  -data-dir "$WORK/data" \
  -hy2-config "$WORK/config/hysteria.yaml" \
  -hy2-bin /nonexistent/hysteria \
  -backup-dir "$WORK/backup" \
  > "$WORK/panel.log" 2>&1 &
PID=$!

cleanup() { kill "$PID" 2>/dev/null; }
trap cleanup EXIT

sleep 2

PW=$(grep -oP '密码:\s+\K\S+' "$WORK/panel.log" || true)
echo "== 管理员随机密码: ${PW:-<未捕获>}"

echo "== 登录"
LOGIN=$(curl -s -c "$WORK/cookies.txt" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}")
echo "   $LOGIN"
CSRF=$(echo "$LOGIN" | grep -oP '"csrfToken":"\K[^"]+')

AUTH="-b $WORK/cookies.txt -H X-CSRF-Token:$CSRF"

echo "== /api/health"
curl -s "$BASE/api/health"; echo

echo "== /api/session"
curl -s $AUTH "$BASE/api/session"; echo

echo "== 创建用户 alice"
curl -s $AUTH -X POST "$BASE/api/users" -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"alicepass","note":"测试用户"}'; echo

echo "== 用户列表"
curl -s $AUTH "$BASE/api/users"; echo

echo "== 用户 HY2 URI"
curl -s $AUTH "$BASE/api/users/1/uri"; echo

echo "== 用户订阅信息"
SUB=$(curl -s $AUTH "$BASE/api/users/1/subscription")
echo "   $SUB"
TOKEN=$(echo "$SUB" | grep -oP '"token":"\K[^"]+')

echo "== 公开订阅（Clash Meta，应只含已启用混淆字段）"
curl -s "$BASE/sub/$TOKEN"

echo "== 公开订阅（URI 文本）"
curl -s "$BASE/sub/$TOKEN?format=uri"

echo "== 踢下线"
curl -s $AUTH -X POST "$BASE/api/users/1/kick"; echo

echo "== 读取服务器配置（结构 + 未识别字段 + 校验结果）"
curl -s $AUTH "$BASE/api/server/config" | head -c 600; echo

echo "== 仅校验（应通过）"
curl -s $AUTH -X POST "$BASE/api/server/config/validate" -H 'Content-Type: application/json' \
  -d '{"config":{"listen":":443","auth":{"type":"userpass","userpass":{"a":"b"}}}}'; echo

echo "== 仅校验（应报错：tls 与 acme 冲突）"
curl -s $AUTH -X POST "$BASE/api/server/config/validate" -H 'Content-Type: application/json' \
  -d '{"config":{"listen":":443","tls":{"cert":"c","key":"k"},"acme":{"domains":["a.com"],"email":"a@b.c","type":"dns","dns":{"name":"cloudflare"}},"auth":{"type":"password","password":"x"}}}'; echo

echo "== 应用新配置"
curl -s $AUTH -X PUT "$BASE/api/server/config" -H 'Content-Type: application/json' \
  -d '{"config":{"listen":":8443","auth":{"type":"password","password":"pw123456"},"trafficStats":{"listen":"127.0.0.1:19999","secret":"testsecret"}}}'; echo

echo "== 应用后的配置文件"
cat "$WORK/config/hysteria.yaml"

echo "== 备份列表"
curl -s $AUTH "$BASE/api/server/backups"; echo

echo "== 原始配置读取"
curl -s $AUTH "$BASE/api/server/config/raw" | head -c 200; echo

echo "== 设置读取 / 写入"
curl -s $AUTH "$BASE/api/settings"; echo
curl -s $AUTH -X PUT "$BASE/api/settings" -H 'Content-Type: application/json' \
  -d '{"serverHost":"example.com","subBaseURL":"https://example.com","mixedPort":7890,"logHysteria":"journal:hysteria-server","logPanel":"","logNginx":"/var/log/nginx/error.log"}'; echo

echo "== 网络：在线 / 连接 / 流量（官方 API 未运行时应为可读错误）"
curl -s $AUTH "$BASE/api/network/online"; echo
curl -s $AUTH "$BASE/api/network/streams"; echo
curl -s $AUTH "$BASE/api/network/traffic"; echo

echo "== 系统状态 / 信息"
curl -s $AUTH "$BASE/api/system/status"; echo
curl -s $AUTH "$BASE/api/system/info"; echo

echo "== 日志（panel）"
curl -s $AUTH "$BASE/api/logs/panel?lines=5" | head -c 400; echo

echo "== 鉴权边界"
printf '   无会话访问受保护接口 -> HTTP %s\n' \
  "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/dashboard")"
printf '   有会话但无 CSRF 头写操作 -> HTTP %s\n' \
  "$(curl -s -o /dev/null -w '%{http_code}' -b "$WORK/cookies.txt" -X POST "$BASE/api/users" \
      -H 'Content-Type: application/json' -d '{"username":"x"}')"
printf '   错误密码登录 -> HTTP %s\n' \
  "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/login" \
      -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrong"}')"

echo "== 冒烟测试结束"