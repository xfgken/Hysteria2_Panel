#!/usr/bin/env bash
#
# 验证「面板生成的 URI / 订阅」能否让官方客户端直接连上。
#
# 用法（在服务器上）：bash verify-client.sh [用户ID，默认 1]
# 关键点：客户端参数全部从面板生成的 URI 中解析，不做任何手工补充。

set -u

BASE=http://127.0.0.1:8080
UID_="${1:-1}"
W=/tmp/hy2client
rm -rf "$W" && mkdir -p "$W"

# 面板管理员密码：优先取环境变量；否则尝试从 journal 中提取首次启动时打印的随机密码。
PW="${ADMIN_PASSWORD:-}"
if [ -z "$PW" ]; then
  PW="$(journalctl -u hy2-panel --no-pager 2>/dev/null | grep -oP '管理员密码:\s+\K\S+' | tail -1)"
fi
if [ -z "$PW" ]; then
  echo "无法获取面板管理员密码，请用 ADMIN_PASSWORD=xxx bash $0 指定" >&2
  exit 1
fi

LOGIN="$(curl -s -c "$W/cookies" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}")"
case "$LOGIN" in
  *csrfToken*) ;;
  *) echo "登录失败：$LOGIN" >&2; exit 1 ;;
esac

SUB="$(curl -s -b "$W/cookies" "$BASE/api/users/$UID_/subscription" | grep -oP '"url":"\K[^"]+')"

# URI 改用公开的纯文本端点获取，避免 JSON 对 & 的转义（\u0026）干扰解析，
# 同时也顺带验证了这个端点本身可用。
URI="$(curl -s "${SUB}?format=uri" | tr -d '\r\n')"

echo "===== 面板生成的 URI ====="
echo "  $URI"
echo "===== 面板生成的订阅地址 ====="
echo "  $SUB"

# ---- 从 URI 解析参数（用 shell 参数展开，避免 sed 分隔符冲突）----
REST="${URI#hysteria2://}"
AUTH="${REST%%@*}"
HOSTPORT="${REST#*@}"
HOSTPORT="${HOSTPORT%%/*}"
QUERY="${URI#*\?}"
QUERY="${QUERY%%#*}"

getp() { echo "$QUERY" | tr '&' '\n' | grep "^$1=" | cut -d= -f2- ; }

OBFS="$(getp obfs)"
OBFSPW="$(getp obfs-password)"
INSECURE="$(getp insecure)"
PIN="$(getp pinSHA256)"
SNI="$(getp sni)"

echo "===== 从 URI 解析出的客户端参数 ====="
printf '  服务器        %s\n' "$HOSTPORT"
printf '  认证          %s\n' "$AUTH"
printf '  混淆          %s / %s\n' "${OBFS:-无}" "${OBFSPW:-无}"
printf '  insecure      %s\n' "${INSECURE:-未设置}"
printf '  pinSHA256     %s\n' "${PIN:-未设置}"
printf '  sni           %s\n' "${SNI:-未设置}"

# ---- 依据 URI 生成客户端配置 ----
{
  echo "server: $HOSTPORT"
  echo "auth: $AUTH"
  if [ -n "$OBFS" ]; then
    echo "obfs:"
    echo "  type: $OBFS"
    echo "  $OBFS:"
    echo "    password: \"$OBFSPW\""
  fi
  echo "tls:"
  [ "$INSECURE" = "1" ] && echo "  insecure: true"
  [ -n "$PIN" ] && echo "  pinSHA256: $PIN"
  [ -n "$SNI" ] && echo "  sni: $SNI"
  echo "socks5:"
  echo "  listen: 127.0.0.1:11082"
} > "$W/client.yaml"

echo "===== 依据 URI 生成的客户端配置 ====="
sed 's/^/  /' "$W/client.yaml"

echo "===== 用官方客户端实测连接（6 秒）====="
timeout 6 /usr/local/bin/hysteria client -c "$W/client.yaml" > "$W/client.log" 2>&1
if grep -q "connected to server" "$W/client.log"; then
  echo "  ✓ 连接成功"
else
  echo "  ✗ 连接失败："
fi
grep -E "connected to server|FATAL|ERROR" "$W/client.log" | sed 's/^/  /' | head -5

echo
echo "===== Clash 订阅内容（代理段）====="
curl -s "$SUB" | sed -n '/^proxies:/,/^proxy-groups:/p' | sed 's/^/  /'