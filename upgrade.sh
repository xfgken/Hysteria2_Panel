#!/bin/sh
# ============================================================================
#  HY2 Panel 升级脚本
#   从 GitHub Release 重新拉取最新版的「面板二进制 + 前端」，原地替换，
#   自动备份旧版，启动失败自动回滚。配置(config/)与数据(data/)绝不碰。
#
#  用法：
#    curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/upgrade.sh | sh
#    sh upgrade.sh --check                        # 只看有没有新版，不改动
#    sh upgrade.sh --panel-bin ./hy2-panel-linux-amd64 --web-tar ./hy2-web.tar.gz
#    sh upgrade.sh --rollback                     # 回滚到上一版
#
#  参数：
#    --check        只检查是否有新版（不改动系统）
#    --rollback     回滚到上一次升级前的版本
#    --force        即使前端资源名相同也重新拉取
#    --panel-bin F  用本地二进制（不走网络）
#    --web-tar F    用本地前端包（不走网络）
#    --no-web       只升级二进制，不动前端
#    --no-restart   只替换文件，不重启服务
# ============================================================================
set -u
REPO="xfgken/Hysteria2_Panel"
BASE="https://github.com/$REPO/releases/latest/download"
APP_DIR="${HY2_APP_DIR:-/opt/hy2-panel}"
UNIT="${HY2_UNIT:-hy2-panel}"
CHECK_ONLY=0
DO_ROLLBACK=0
FORCE=0
NO_WEB=0
NO_RESTART=0
PANEL_BIN=""
WEB_TAR=""

# ---------- 输出 ----------
if [ -t 1 ]; then
  C_R=$(printf '\033[31m'); C_G=$(printf '\033[32m'); C_Y=$(printf '\033[33m')
  C_B=$(printf '\033[36m'); C_D=$(printf '\033[2m');  C_0=$(printf '\033[0m')
else
  C_R=''; C_G=''; C_Y=''; C_B=''; C_D=''; C_0=''
fi
step() { printf '%s\n' "${C_B}==>$C_0 $1"; }
ok()   { printf '%s\n' "${C_G}  ✓${C_0} $1"; }
info() { printf '%s\n' "${C_D}    $1${C_0}"; }
warn() { printf '%s\n' "${C_Y}  !${C_0} $1"; }
die()  { printf '%s\n' "${C_R}  ✗${C_0} $1"; exit 1; }

usage() {
  sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
}

# ---------- 参数 ----------
while [ $# -gt 0 ]; do
  case "$1" in
    --check) CHECK_ONLY=1 ;;
    --rollback) DO_ROLLBACK=1 ;;
    --force) FORCE=1 ;;
    --no-web) NO_WEB=1 ;;
    --no-restart) NO_RESTART=1 ;;
    --panel-bin) shift; PANEL_BIN="${1:-}" ;;
    --web-tar) shift; WEB_TAR="${1:-}" ;;
    -h|--help) usage ;;
    *) die "未知参数：$1（--help 看用法）" ;;
  esac
  shift
done

[ "$(id -u)" = "0" ] || die "需要 root 权限运行"
command -v curl >/dev/null 2>&1 || die "缺少 curl（apt install curl / yum install curl）"
command -v tar  >/dev/null 2>&1 || die "缺少 tar"
[ -d "$APP_DIR" ] || die "没找到已有安装：$APP_DIR（新服务器请先跑 install.sh）"
[ -x "$APP_DIR/hy2-panel" ] || die "没找到面板二进制：$APP_DIR/hy2-panel"

# 端口：从 systemd 单元的 -listen 参数里读（回退 8080）
PORT="8080"
_unit_txt="$(systemctl cat "$UNIT" 2>/dev/null || cat "/etc/systemd/system/$UNIT.service" 2>/dev/null || true)"
_listen="$(printf '%s' "$_unit_txt" | grep -o -- '-listen[= ][^ \\]*' | head -1 | sed 's/.*[-= ]//')"
[ -n "$_listen" ] && PORT="${_listen##*:}"

# 当前前端资源（用来判断版本）
assets_now() { ls "$APP_DIR/web/dist/assets" 2>/dev/null | sort | tr '\n' ' '; }

# ============================ 回滚 ============================
if [ "$DO_ROLLBACK" = "1" ]; then
  step "回滚到上一版"
  [ -f "$APP_DIR/hy2-panel.prev" ] || die "没有找到上一版二进制（$APP_DIR/hy2-panel.prev）"
  last="$(ls -1 "$APP_DIR/backup"/upgrade-*.tgz 2>/dev/null | sort | tail -1)"
  systemctl stop "$UNIT" 2>/dev/null || true
  cp -f "$APP_DIR/hy2-panel" "$APP_DIR/hy2-panel.bad" 2>/dev/null || true
  install -m 755 "$APP_DIR/hy2-panel.prev" "$APP_DIR/hy2-panel"
  if [ -n "$last" ]; then
    rm -rf "$APP_DIR/web/dist"
    tar xzf "$last" -C "$APP_DIR/web" 2>/dev/null || warn "前端未能从 $last 恢复"
    info "前端已从 $(basename "$last") 恢复"
  fi
  systemctl start "$UNIT"
  sleep 3
  ok "已回滚；服务状态：$(systemctl is-active "$UNIT")"
  info "当前前端资源：$(assets_now)"
  exit 0
fi

# ============================ 取新版 ============================
case "$(uname -m)" in
  x86_64|amd64) ASSET="hy2-panel-linux-amd64" ;;
  aarch64|arm64) ASSET="hy2-panel-linux-arm64" ;;
  *) die "不支持的架构：$(uname -m)" ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

step "获取最新版"
if [ -n "$PANEL_BIN" ]; then
  cp "$PANEL_BIN" "$TMP/panel"
  info "二进制：本地 $PANEL_BIN"
elif [ "$CHECK_ONLY" = "1" ]; then
  info "二进制：$BASE/$ASSET（--check 不下载）"
else
  curl -fsSL --retry 3 --connect-timeout 20 -o "$TMP/panel" "$BASE/$ASSET" || die "下载二进制失败：$BASE/$ASSET"
  info "二进制：已下载 $ASSET"
fi

NEW_ASSETS=""
# 前端包很小（百 KB 级），--check 也拉下来用于版本比对；二进制在 --check 下不下载
if [ "$NO_WEB" = "0" ] || [ "$CHECK_ONLY" = "1" ]; then
  if [ -n "$WEB_TAR" ]; then
    cp "$WEB_TAR" "$TMP/web.tgz"
    info "前端包：本地 $WEB_TAR"
  else
    if curl -fsSL --retry 3 --connect-timeout 20 -o "$TMP/web.tgz" "$BASE/hy2-web.tar.gz"; then
      info "前端包：已下载 hy2-web.tar.gz"
    else
      if [ "$CHECK_ONLY" = "1" ]; then warn "前端包下载失败，无法比对版本"
      else die "下载前端包失败"; fi
      rm -f "$TMP/web.tgz"
    fi
  fi
  if [ -f "$TMP/web.tgz" ]; then
    mkdir -p "$TMP/x"
    if tar xzf "$TMP/web.tgz" -C "$TMP/x"; then
      if [ -d "$TMP/x/dist/assets" ]; then
        NEW_ASSETS="$(ls "$TMP/x/dist/assets" | sort | tr '\n' ' ')"
      elif [ -d "$TMP/x/assets" ]; then
        NEW_ASSETS="$(ls "$TMP/x/assets" | sort | tr '\n' ' ')"
      fi
    else
      [ "$CHECK_ONLY" = "1" ] && warn "前端包解压失败" || die "前端包解压失败"
    fi
  fi
fi

if [ -f "$TMP/panel" ]; then
  sz=$(wc -c < "$TMP/panel")
  [ "$sz" -gt 1048576 ] || die "拿到的二进制太小（$sz 字节），可能不是有效文件"
  head -c 4 "$TMP/panel" | od -An -c | grep -q '177   E   L   F' || die "拿到的不是 Linux ELF 可执行文件"
fi

OLD_ASSETS="$(assets_now)"
step "版本对比"
info "当前前端：$OLD_ASSETS"
[ -n "$NEW_ASSETS" ] && info "最新前端：$NEW_ASSETS"
if [ -n "$NEW_ASSETS" ] && [ "$NEW_ASSETS" = "$OLD_ASSETS" ] && [ "$FORCE" != "1" ]; then
  ok "已经是最新版（前端资源名一致），无需升级"
  exit 0
fi

if [ "$CHECK_ONLY" = "1" ]; then
  if [ -z "$NEW_ASSETS" ]; then
    warn "无法比对前端版本（未取到发布包），去掉 --check 直接升级即可"
  else
    ok "有新版本可升级（去掉 --check 即真正升级）"
  fi
  exit 0
fi

# ============================ 备份 ============================
step "备份当前版本"
TS="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$APP_DIR/backup"
cp -f "$APP_DIR/hy2-panel" "$APP_DIR/hy2-panel.prev"
if [ -d "$APP_DIR/web/dist" ]; then
  tar czf "$APP_DIR/backup/upgrade-$TS.tgz" -C "$APP_DIR/web" dist 2>/dev/null || warn "前端备份失败（继续）"
fi
ok "旧版已备份（hy2-panel.prev + backup/upgrade-$TS.tgz）"

# ============================ 替换 ============================
step "替换文件"
if [ "$NO_RESTART" = "1" ]; then
  info "--no-restart：跳过停服务"
else
  systemctl stop "$UNIT" 2>/dev/null || true
fi
install -m 755 "$TMP/panel" "$APP_DIR/hy2-panel"
chown "$(stat -c '%u:%g' "$APP_DIR" 2>/dev/null || echo 'root:root')" "$APP_DIR/hy2-panel" 2>/dev/null || true
ok "面板二进制已替换"

if [ "$NO_WEB" = "0" ] && [ -f "$TMP/web.tgz" ]; then
  rm -rf "$APP_DIR/web/dist"
  mkdir -p "$APP_DIR/web/dist" "$APP_DIR/templates"
  if [ -d "$TMP/x/dist" ]; then
    cp -R "$TMP/x/dist/." "$APP_DIR/web/dist/"
  else
    cp -R "$TMP/x/." "$APP_DIR/web/dist/"
  fi
  [ -d "$TMP/x/templates" ] && cp -R "$TMP/x/templates/." "$APP_DIR/templates/"
  ok "前端资源已替换：$(assets_now)"
fi

# ============================ 启动 + 健康检查 ============================
if [ "$NO_RESTART" = "1" ]; then
  ok "未重启服务（--no-restart），请自行 systemctl restart $UNIT"
  exit 0
fi

step "启动并健康检查"
systemctl start "$UNIT"
HEALTH=0
i=1
while [ $i -le 10 ]; do
  sleep 1
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$PORT/" 2>/dev/null || echo 000)"
  if [ "$code" = "200" ]; then HEALTH=1; break; fi
  i=$((i+1))
done

if [ "$HEALTH" = "1" ]; then
  ok "升级完成：面板 HTTP 200（端口 $PORT）"
  info "前端资源：$(assets_now)"
  info "配置与数据未改动：$APP_DIR/config、$APP_DIR/data"
  exit 0
fi

warn "启动后健康检查失败，正在回滚"
install -m 755 "$APP_DIR/hy2-panel.prev" "$APP_DIR/hy2-panel"
last="$(ls -1 "$APP_DIR/backup"/upgrade-*.tgz 2>/dev/null | sort | tail -1)"
[ -n "$last" ] && { rm -rf "$APP_DIR/web/dist"; tar xzf "$last" -C "$APP_DIR/web" 2>/dev/null || true; }
systemctl restart "$UNIT"
sleep 3
if [ "$(systemctl is-active "$UNIT")" = "active" ]; then
  ok "已回滚到旧版，服务正常"
else
  warn "回滚后服务仍异常，请查看：journalctl -u $UNIT -n 50"
fi
info "诊断：journalctl -u $UNIT -n 50 --no-pager"
exit 1
