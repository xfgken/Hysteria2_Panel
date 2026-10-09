#!/usr/bin/env bash
#
# 前后端联调测试：验证 Panel 是否正确提供前端静态资源与 API。
#
# 用法：bash scripts/e2e-web-test.sh [二进制路径] [前端目录]

set -u

BIN="${1:-/root/hy2build/hy2-panel}"
WEB="${2:-/root/hy2build/web/dist}"
WORK=/tmp/hy2e2e
PORT=18100
BASE="http://127.0.0.1:${PORT}"

pkill -f 'hy2-panel -listen' 2>/dev/null
rm -rf "$WORK"
mkdir -p "$WORK/config" "$WORK/data"

cat > "$WORK/config/hysteria.yaml" <<'YAML'
listen: :443
auth:
  type: userpass
  userpass:
    demo: demopass
trafficStats:
  listen: 127.0.0.1:19999
  secret: testsecret
YAML

"$BIN" -listen "127.0.0.1:${PORT}" \
  -data-dir "$WORK/data" \
  -hy2-config "$WORK/config/hysteria.yaml" \
  -hy2-bin /nonexistent/hysteria \
  -backup-dir "$WORK/backup" \
  -web-dir "$WEB" \
  > "$WORK/panel.log" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT
sleep 2

echo "===== 前端静态资源 ====="
curl -s -o /dev/null -w '首页           HTTP %{http_code}  %{content_type}  %{size_download} B\n' "$BASE/"
curl -s -o /dev/null -w 'SPA 回退 /users HTTP %{http_code}  %{content_type}\n' "$BASE/users"

JS=$(curl -s "$BASE/" | grep -oP 'assets/\K[^"]+\.js' | head -1)
CSS=$(curl -s "$BASE/" | grep -oP 'assets/\K[^"]+\.css' | head -1)
echo "资源索引        js=$JS  css=$CSS"
curl -s -o /dev/null -w 'JS 包          HTTP %{http_code}  %{content_type}  %{size_download} B\n' "$BASE/assets/$JS"
curl -s -o /dev/null -w 'CSS 包         HTTP %{http_code}  %{content_type}  %{size_download} B\n' "$BASE/assets/$CSS"

echo
echo "===== 接口与鉴权 ====="
echo -n '健康检查        '; curl -s "$BASE/api/health"; echo
printf '未登录访问 API  HTTP %s\n' "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/dashboard")"

PW=$(grep -oP '密码:\s+\K\S+' "$WORK/panel.log" || true)
printf '首次启动密码    %s\n' "${PW:-<未捕获>}"

LOGIN=$(curl -s -c "$WORK/cookies.txt" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}")
echo -n '登录            '; echo "$LOGIN"

AUTH="-b $WORK/cookies.txt"
echo -n '仪表盘          '; curl -s $AUTH "$BASE/api/dashboard" | head -c 220; echo
echo -n '服务器配置      '; curl -s $AUTH "$BASE/api/server/config" | head -c 220; echo
echo -n '系统状态        '; curl -s $AUTH "$BASE/api/system/status" | head -c 220; echo
echo -n '日志(Panel)     '; curl -s $AUTH "$BASE/api/logs/panel?lines=3" | head -c 200; echo

echo
echo "===== 性能抽样 ====="
for i in 1 2 3; do
  curl -s -o /dev/null -w "  第 ${i} 次请求首页耗时 %{time_total}s\n" "$BASE/"
done

echo
echo "===== 联调测试结束 ====="