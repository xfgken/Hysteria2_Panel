#!/usr/bin/env bash
#
# 触发一次配置应用，从而让账号名按规则重新生成。
#
# 用法（在服务器上）：ADMIN_PASSWORD=xxx bash rotate-identity.sh

set -u

BASE=http://127.0.0.1:8080
PW="${ADMIN_PASSWORD:-admin123}"
C=/tmp/rot-cookie.txt

curl -s -c "$C" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" >/dev/null

CSRF="$(curl -s -b "$C" "$BASE/api/session" | grep -oP '"csrfToken":"\K[^"]+')"

echo "--- 应用前的账号名 ---"
curl -s -b "$C" "$BASE/api/users" | grep -oP '"username":"\K[^"]+'

# 取出当前配置，再原样提交一次（apply 会重新生成账号名）
curl -s -b "$C" "$BASE/api/server/config" \
  | python3 -c "import json,sys; d=json.load(sys.stdin); json.dump({'config': d['config']}, sys.stdout)" \
  > /tmp/rot-body.json

echo "--- 应用结果 ---"
curl -s -b "$C" -H "X-CSRF-Token: $CSRF" -X PUT "$BASE/api/server/config" \
  -H 'Content-Type: application/json' --data-binary @/tmp/rot-body.json
echo

sleep 2
echo "--- 应用后的账号名 ---"
curl -s -b "$C" "$BASE/api/users" | grep -oP '"username":"\K[^"]+'

echo "--- 配置文件中的 userpass ---"
sed -n '/^auth:/,/^trafficStats:/p' /opt/hy2-panel/config/hysteria.yaml | head -5

echo "--- 服务状态 ---"
systemctl is-active hysteria-server hy2-panel