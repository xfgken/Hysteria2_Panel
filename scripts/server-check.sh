#!/usr/bin/env bash
#
# HY2 Panel 服务器体检脚本
#
# 用法（在目标服务器上）：bash server-check.sh
# 用于部署后快速定位问题，输出结构化报告。

set -u

PANEL_DIR=/opt/hy2-panel
HY2_UNIT=hysteria-server
PANEL_UNIT=hy2-panel

hr() { printf '\n===== %s =====\n' "$1"; }

hr "1. 服务状态"
for u in "$HY2_UNIT" "$PANEL_UNIT" nginx; do
  printf '%-18s active=%-12s enabled=%s\n' "$u" \
    "$(systemctl is-active "$u" 2>/dev/null || echo -)" \
    "$(systemctl is-enabled "$u" 2>/dev/null || echo -)"
done

hr "2. 官方 Core 日志（最近 25 行）"
journalctl -u "$HY2_UNIT" -n 25 --no-pager 2>/dev/null | sed 's/^/  /'

hr "3. Panel 日志（最近 30 行）"
journalctl -u "$PANEL_UNIT" -n 30 --no-pager 2>/dev/null | sed 's/^/  /'

hr "4. 初始凭据"
PW="$(journalctl -u "$PANEL_UNIT" --no-pager 2>/dev/null | grep -oP '管理员密码:\s+\K\S+' | tail -1 || true)"
HY2PW="$(journalctl -u "$PANEL_UNIT" --no-pager 2>/dev/null | grep -oP 'HY2 用户密码:\s+\K\S+' | tail -1 || true)"
if [ -n "$PW" ]; then
  echo "  面板管理员密码: $PW"
else
  echo "  （journalctl 中未找到；尝试 panel.log）"
  grep -oP '管理员密码:\s+\K\S+' "$PANEL_DIR/data/panel.log" 2>/dev/null | tail -1 | sed 's/^/  /'
fi
if [ -n "$HY2PW" ]; then
  echo "  HY2 初始用户密码: $HY2PW"
fi

hr "5. Panel 健康检查"
curl -s --max-time 8 http://127.0.0.1:8080/api/health || echo "  （无法访问）"
echo

hr "6. 监听端口"
ss -ltnp 2>/dev/null | grep -E ':8080' | sed 's/^/  /' || echo "  8080 未监听"
ss -lunp 2>/dev/null | grep -E ':443' | sed 's/^/  /' || echo "  UDP 443 未监听"

hr "7. 版本"
printf '  core : %s\n' "$(/usr/local/bin/hysteria version 2>/dev/null | head -1 || echo 未安装)"
printf '  panel: %s\n' "$("$PANEL_DIR/hy2-panel" help >/dev/null 2>&1 && echo 可执行 || echo 不可执行)"

hr "8. 官方配置"
cat "$PANEL_DIR/config/hysteria.yaml" 2>/dev/null | sed 's/^/  /'

hr "9. 权限测试（Panel 以非特权用户运行）"
if id -u hy2-panel >/dev/null 2>&1; then
  echo -n "  写 /usr/local/bin（更新 Core 需要）: "
  if su -s /bin/sh hy2-panel -c 'touch /usr/local/bin/.hy2pw 2>/dev/null && rm -f /usr/local/bin/.hy2pw' 2>/dev/null; then
    echo "可写"
  else
    echo "不可写  <== 更新功能会失败"
  fi

  echo -n "  重启 hysteria 服务（systemctl）: "
  OUT="$(su -s /bin/sh hy2-panel -c "systemctl restart $HY2_UNIT" 2>&1 || true)"
  if [ -z "$OUT" ]; then
    echo "成功"
  else
    echo "失败  <== ${OUT//$'\n'/ }"
  fi

  echo -n "  读取官方配置: "
  su -s /bin/sh hy2-panel -c "test -r $PANEL_DIR/config/hysteria.yaml" 2>/dev/null \
    && echo "可读" || echo "不可读"
fi

hr "10. systemd 单元关键指令"
for u in "$HY2_UNIT" "$PANEL_UNIT"; do
  echo "  --- $u ---"
  systemctl cat "$u" 2>/dev/null | grep -E '^(User|ExecStart|ProtectSystem|ReadWritePaths|NoNewPrivileges|AmbientCapabilities)' | sed 's/^/    /'
done

hr "体检结束"