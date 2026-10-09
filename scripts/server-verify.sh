#!/usr/bin/env bash
#
# HY2 Panel 部署后端到端功能验证
#
# 用法（在服务器上）：bash server-verify.sh
# 覆盖：健康检查、登录、版本解析、用户增删同步、实时数据、订阅渲染、更新检查。

set -u

BASE=http://127.0.0.1:8080
CFG=/opt/hy2-panel/config/hysteria.yaml
WORK=/tmp/hy2verify
mkdir -p "$WORK"

pass=0
fail=0
ok() { printf '\033[32m  ✓\033[0m %s\n' "$1"; pass=$((pass + 1)); }
no() { printf '\033[31m  ✗\033[0m %s\n' "$1"; fail=$((fail + 1)); }
hr() { printf '\n\033[36m===== %s =====\033[0m\n' "$1"; }

# ---------------------------------------------------------------------------
hr "1. Panel 健康检查"
HEALTH="$(curl -s --max-time 10 "$BASE/api/health")"
echo "  $HEALTH"
case "$HEALTH" in
  *'"hysteria_api":"ok"'*) ok "官方 Traffic Stats API 连通" ;;
  *) no "官方 API 不可达" ;;
esac
case "$HEALTH" in
  *'"database":"ok"'*) ok "数据库正常" ;;
  *) no "数据库异常" ;;
esac

# ---------------------------------------------------------------------------
hr "2. 登录与会话"
PW="${ADMIN_PASSWORD:-}"
if [ -z "$PW" ]; then
  PW="$(journalctl -u hy2-panel --no-pager 2>/dev/null | grep -oP '管理员密码:\s+\K\S+' | tail -1 || true)"
fi
if [ -z "$PW" ]; then
  no "无法获取管理员密码（可用 ADMIN_PASSWORD=xxx 指定）"
else
  ok "取得管理员密码"
fi

LOGIN="$(curl -s -c "$WORK/cookies" -X POST "$BASE/api/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}")"
CSRF="$(echo "$LOGIN" | grep -oP '"csrfToken":"\K[^"]+' || true)"
if [ -n "$CSRF" ]; then ok "登录成功并取得 CSRF Token"; else no "登录失败: $LOGIN"; fi

AUTH="-b $WORK/cookies -H X-CSRF-Token:$CSRF"

# ---------------------------------------------------------------------------
hr "3. 官方 Core 版本解析（面板 vs 二进制）"
DASH_V="$(curl -s $AUTH "$BASE/api/dashboard" | grep -oP '"coreVersion":"\K[^"]*' || true)"
REAL_V="$(/usr/local/bin/hysteria version 2>/dev/null | sed -n 's/^Version:[[:space:]]*//p' | head -1)"
printf '  面板显示: %s\n  二进制实际: %s\n' "${DASH_V:-<空>}" "${REAL_V:-<空>}"
if [ -n "$DASH_V" ] && [ "$DASH_V" = "$REAL_V" ]; then
  ok "版本解析一致（不再把 ASCII logo 当成版本号）"
else
  no "版本解析不一致"
fi

# ---------------------------------------------------------------------------
hr "4. 账号（单用户模式）与配置同步"
BEFORE="$(systemctl show -p ActiveEnterTimestamp --value hysteria-server)"

LIST="$(curl -s $AUTH "$BASE/api/users")"
NEW_ID="$(echo "$LIST" | grep -oP '"id":\K[0-9]+' | head -1)"
NEW_NAME="$(echo "$LIST" | grep -oP '"username":"\K[^"]+' | head -1)"

if [ -n "$NEW_ID" ]; then
  printf '  已有账号：%s (id=%s)\n' "$NEW_NAME" "$NEW_ID"
  # 单用户模式：再次创建应被拒绝
  DUP="$(curl -s $AUTH -X POST "$BASE/api/users" -H 'Content-Type: application/json' -d '{"password":"x"}')"
  case "$DUP" in
    *"仅支持一个账号"*) ok "单用户模式正确拒绝重复创建" ;;
    *) no "重复创建未被拒绝: $DUP" ;;
  esac
else
  CREATE="$(curl -s $AUTH -X POST "$BASE/api/users" -H 'Content-Type: application/json' \
    -d '{"password":"verify-pass-123","note":"部署验证"}')"
  echo "  $CREATE"
  NEW_ID="$(echo "$CREATE" | grep -oP '"id":\K[0-9]+' | head -1)"
  NEW_NAME="$(echo "$CREATE" | grep -oP '"username":"\K[^"]+' | head -1)"
  printf '  创建账号：%s (id=%s)\n' "$NEW_NAME" "$NEW_ID"
fi

case "$NEW_NAME" in
  hysteria2-??????) ok "账号名格式为 hysteria2-xxxxxx" ;;
  *) no "账号名格式不符合 hysteria2-xxxxxx：$NEW_NAME" ;;
esac

# 改密码以触发一次配置同步（会重启官方服务）
SYNC="$(curl -s $AUTH -X PUT "$BASE/api/users/$NEW_ID" -H 'Content-Type: application/json' \
  -d '{"password":"verify-pass-123"}')"
if echo "$SYNC" | grep -q '"syncError":""'; then
  ok "同步到官方配置无报错"
else
  no "同步报错: $(echo "$SYNC" | grep -oP '"syncError":"\K[^"]*')"
fi

sleep 3
if grep -q "$NEW_NAME" "$CFG"; then ok "账号已写入 config.yaml"; else no "config.yaml 中没有该账号"; fi

AFTER="$(systemctl show -p ActiveEnterTimestamp --value hysteria-server)"
if [ "$BEFORE" != "$AFTER" ]; then
  ok "hysteria 服务已被重启（Panel 具备服务控制权限）"
else
  no "服务未重启（可能缺少权限）"
fi
if [ "$(systemctl is-active hysteria-server)" = "active" ]; then
  ok "重启后服务仍为 active"
else
  no "重启后服务未处于 active"
fi

# ---------------------------------------------------------------------------
hr "5. 实时数据（官方 API）"
ONLINE="$(curl -s $AUTH "$BASE/api/network/online")"
echo "  online: $ONLINE"
if echo "$ONLINE" | grep -q '"items"'; then ok "在线用户接口可用"; else no "在线用户接口异常"; fi

TRAFFIC="$(curl -s $AUTH "$BASE/api/network/traffic")"
if echo "$TRAFFIC" | grep -q '"realtimeError":""'; then ok "流量接口实时数据可用"; else no "流量接口提示错误"; fi

# ---------------------------------------------------------------------------
hr "6. 订阅（HY2 URI + Clash Meta）"
SUBINFO="$(curl -s $AUTH "$BASE/api/users/$NEW_ID/subscription")"
TOKEN="$(echo "$SUBINFO" | grep -oP '"token":"\K[^"]+' || true)"
if [ -n "$TOKEN" ]; then ok "取得订阅 Token"; else no "订阅 Token 获取失败"; fi

URI_TXT="$(curl -s "$BASE/sub/$TOKEN?format=uri")"
echo "  URI: $URI_TXT"
if echo "$URI_TXT" | grep -q '^hysteria2://'; then ok "HY2 URI 格式正确"; else no "HY2 URI 异常"; fi

CLASH="$(curl -s "$BASE/sub/$TOKEN")"
if echo "$CLASH" | grep -q 'type: hysteria2'; then ok "Clash Meta 订阅可渲染"; else no "Clash 订阅渲染失败"; fi

# ---------------------------------------------------------------------------
hr "7. 配置校验接口（含 ACME 替换路径）"
# 提交一份带 acme 的配置：校验过程不应真的去申请证书
VALIDATE="$(curl -s $AUTH -X POST "$BASE/api/server/config/validate" \
  -H 'Content-Type: application/json' \
  -d '{"config":{"listen":":8443","acme":{"domains":["example.org"],"email":"a@example.org","type":"http","http":{"altPort":80}},"auth":{"type":"password","password":"x"}}}')"
echo "  $VALIDATE" | head -c 400; echo
if echo "$VALIDATE" | grep -q '"ok":'; then ok "校验接口返回结构正常"; else no "校验接口异常"; fi

hr "8. 更新检查（版本比较）"
CHECK="$(curl -s --max-time 60 $AUTH "$BASE/api/system/update/check")"
echo "  $CHECK" | head -c 400; echo
if echo "$CHECK" | grep -q '"upToDate":true'; then
  ok "版本比较正确（已是最新）"
elif echo "$CHECK" | grep -q '"upToDate":false'; then
  ok "检测到新版本（当前版本解析正确）"
else
  no "更新检查异常"
fi

# ---------------------------------------------------------------------------
hr "9. 单用户模式：唯一账号不可删除"
DEL="$(curl -s -o /dev/null -w '%{http_code}' $AUTH -X DELETE "$BASE/api/users/$NEW_ID")"
printf '  删除唯一账号 -> HTTP %s\n' "$DEL"
if [ "$DEL" = "409" ]; then
  ok "正确拒绝删除唯一账号（避免 userpass 为空导致服务起不来）"
else
  no "删除唯一账号应返回 409，实际 $DEL"
fi
if [ "$(systemctl is-active hysteria-server)" = "active" ]; then ok "服务仍正常"; else no "服务异常"; fi

# ---------------------------------------------------------------------------
printf '\n\033[36m===== 汇总 =====\033[0m\n'
printf '  通过 %d 项，失败 %d 项\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1