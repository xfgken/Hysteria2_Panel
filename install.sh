#!/usr/bin/env bash
# ============================================================================
# HY2 Panel 一键安装脚本
# ----------------------------------------------------------------------------
# 在干净的 Debian / Ubuntu / CentOS / Rocky / Alma / Fedora / Alpine 上：
#   1) 装好基础依赖（curl / tar / unzip / ca-certificates …）
#   2) 装官方 Hysteria 2 Core 到 /usr/local/bin/hysteria
#   3) 建立 hy2-panel 用户与 /opt/hy2-panel 目录树
#   4) 部署 Panel 二进制 + 前端静态资源
#   5) 生成本地配置（随机 obfs 密码 / 流量统计密钥，可选 ACME 域名）
#   6) 写入并启动两个 systemd 单元：hysteria-server、hy2-panel
#   7) 放行防火墙端口，可选开启 BBR
#   8) 打印面板地址与初始管理员密码
#
# 用法（root 权限）：
#   sh install.sh                       # 交互式：会问你要管理员用户名与密码
#   sh install.sh -y                    # 全自动：密码随机生成，完成后打印
#   sh install.sh --admin-user me --admin-password 'xxx'   # 直接指定账号密码
#   sh install.sh --domain hy.example.com --email me@example.com
#   sh install.sh --check               # 只检查环境并打印计划，不改动系统
#   sh install.sh --uninstall           # 卸载（保留数据）
#   sh install.sh --help
#
# 从网络一键安装（参数完全一样，没给账号密码时会交互询问）：
#   curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s --
# ============================================================================

set -u

# ---------------------------- 默认参数 ----------------------------
PANEL_PORT="8080"
HY2_PORT="443"
ACME_ALT_PORT="80"
DOMAIN=""
EMAIL=""
ADMIN_USER="admin"
ADMIN_PASS=""
INITIAL_USER="user1"
PANEL_BIN=""
PANEL_URL=""
WEB_DIR=""
WEB_TAR=""
WITH_HYSTERIA=1
WITH_FIREWALL=1
WITH_BBR=1
CHECK_ONLY=0
ASSUME_YES=0
UNINSTALL=0
PURGE=0

# GitHub 仓库（一键命令从这里拉 install.sh 与发布产物）
REPO="xfgken/Hysteria2_Panel"

APP_DIR="/opt/hy2-panel"
BIN_PATH="/usr/local/bin/hysteria"
UNIT_HY="hysteria-server"
UNIT_PANEL="hy2-panel"
RUN_USER="hy2-panel"

# ---------------------------- 输出小工具 ----------------------------
say()  { printf '%s\n' "$*"; }
step() { printf '\n\033[36m==>\033[0m %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
ok()   { printf '    \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '    \033[33m!\033[0m %s\n' "$*"; }
die()  { printf '\n\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

confirm() {
  [ "$ASSUME_YES" = "1" ] && return 0
  printf '    \033[33m?\033[0m %s [y/N] ' "$1"
  read -r ans || ans=""
  case "$ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

need_root() {
  [ "$(id -u)" = "0" ] || die "请用 root 运行（sudo sh install.sh）"
}

# ---------------------------- 参数解析 ----------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --port)        PANEL_PORT="${2:-}"; shift 2 ;;
    --hy2-port)    HY2_PORT="${2:-}"; shift 2 ;;
    --domain)      DOMAIN="${2:-}"; shift 2 ;;
    --email)       EMAIL="${2:-}"; shift 2 ;;
    --admin-user)  ADMIN_USER="${2:-}"; shift 2 ;;
    --admin-password) ADMIN_PASS="${2:-}"; shift 2 ;;
    --initial-user) INITIAL_USER="${2:-}"; shift 2 ;;
    --panel-bin)   PANEL_BIN="${2:-}"; shift 2 ;;
    --panel-url)   PANEL_URL="${2:-}"; shift 2 ;;
    --web-dir)     WEB_DIR="${2:-}"; shift 2 ;;
    --web-tar)     WEB_TAR="${2:-}"; shift 2 ;;
    --no-hysteria) WITH_HYSTERIA=0; shift ;;
    --no-firewall) WITH_FIREWALL=0; shift ;;
    --no-bbr)      WITH_BBR=0; shift ;;
    --check)       CHECK_ONLY=1; shift ;;
    -y|--yes)      ASSUME_YES=1; shift ;;
    --uninstall)   UNINSTALL=1; shift ;;
    --purge)       PURGE=1; shift ;;
    -h|--help)
      if [ -r "$0" ]; then
        sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
      else
        say "HY2 Panel 一键安装（从管道运行）"
        say "  curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s -- [参数]"
        say '  不给账号密码时会交互询问；完全免交互用 -y';
        say '  其它：--admin-user 名称 --admin-password 密码 --domain 域名 --email 邮箱 --hy2-port 端口'
      fi
      exit 0 ;;
    *) die "未知参数：$1（--help 看用法）" ;;
  esac
done

[ "$CHECK_ONLY" = "1" ] || need_root

# ---------------------------- 交互：管理员账号 ----------------------------
# 即使脚本从管道（curl | sh）进来，也从 /dev/tty 读数，保证有终端就能输入
TTY=/dev/tty

have_tty() { [ -r "$TTY" ] && [ -w "$TTY" ]; }

# ask_line 提示 默认值 -> 结果进 REPLY
ask_line() {
  printf '    %s' "$1" > "$TTY"
  [ -n "${2:-}" ] && printf ' [%s]' "$2" > "$TTY"
  printf ': ' > "$TTY"
  REPLY=''
  IFS= read -r REPLY < "$TTY" || REPLY=''
  [ -z "$REPLY" ] && [ -n "${2:-}" ] && REPLY="$2"
}

# ask_secret 提示 -> 结果进 REPLY（关回显）
ask_secret() {
  printf '    %s: ' "$1" > "$TTY"
  stty -echo < "$TTY" 2>/dev/null || true
  REPLY=''
  IFS= read -r REPLY < "$TTY" || REPLY=''
  stty echo < "$TTY" 2>/dev/null || true
  printf '\n' > "$TTY"
}

# 密码转 systemd 安全形式：双引号包裹、$ 写成 $$
prepare_pass_unit() {
  ADMIN_PASS_UNIT=''
  [ -n "$ADMIN_PASS" ] || return 0
  pu=$(printf '%s' "$ADMIN_PASS" | sed 's/\$/$$/g')
  ADMIN_PASS_UNIT="\"$pu"\"
}

configure_admin() {
  if [ -n "$ADMIN_PASS" ]; then
    prepare_pass_unit
    ok "管理员账号：$ADMIN_USER（密码来自参数）"
    return 0
  fi
  if [ "$CHECK_ONLY" = "1" ]; then
    info "将交互式询问管理员账号与密码（也可用 --admin-user/--admin-password 直接指定）"
    return 0
  fi
  if [ "$ASSUME_YES" = "1" ] || ! have_tty; then
    warn "非交互模式：管理员密码由面板随机生成，完成后会打印"
    return 0
  fi

  step "设置管理员账号"
  info "直接回车用默认值；密码输入时不显示"
  n=0
  while [ "$n" -lt 3 ]; do
    n=$((n + 1))
    ask_line "管理员用户名" "${ADMIN_USER:-admin}"
    ADMIN_USER=${REPLY:-admin}
    ask_secret "管理员密码（至少 8 位，留空则随机生成）"
    p1=$REPLY
    if [ -z "$p1" ]; then
      info "密码留空 —— 由面板随机生成，完成后会打印"
      return 0
    fi
    if [ ${#p1} -lt 8 ]; then
      warn "密码至少 8 位，请重试"
      continue
    fi
    if printf '%s' "$p1" | grep -q '["\]'; then
      warn "密码不能含双引号或反斜杠（会破坏 systemd 参数），请重试"
      continue
    fi
    ask_secret "再输入一次确认"
    if [ "$p1" != "$REPLY" ]; then
      warn "两次输入不一致，请重试"
      continue
    fi
    ADMIN_PASS=$p1
    prepare_pass_unit
    ok "管理员账号：$ADMIN_USER（密码已设置）"
    return 0
  done
  warn "连续 3 次未设置成功，改用面板随机密码"
}
# ---------------------------- 系统探测 ----------------------------
OS_ID=""; OS_LIKE=""; PKG=""
ARCH=""
SYSTEMD=0

detect() {
  step "探测系统环境"
  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_ID="${ID:-unknown}"
    OS_LIKE="${ID_LIKE:-$OS_ID}"
  fi
  info "发行版：${OS_ID}${OS_LIKE:+ ($OS_LIKE)}"

  for c in apt-get dnf yum apk zypper; do
    if command -v "$c" >/dev/null 2>&1; then PKG="$c"; break; fi
  done
  [ -n "$PKG" ] || die "未识别的包管理器（支持 apt / dnf / yum / apk / zypper）"
  ok "包管理器：$PKG"

  case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|armv7)  ARCH="armv7" ;;
    i386|i686)      ARCH="386"   ;;
    *) die "暂不支持的架构：$(uname -m)" ;;
  esac
  ok "CPU 架构：$(uname -m) -> $ARCH"

  if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    SYSTEMD=1; ok "systemd：可用"
  else
    warn "systemd 不可用 —— 无法自动托管服务，将只安装文件"
  fi

  if [ "$CHECK_ONLY" = "1" ]; then
    info "（--check 模式：只检查，不改动系统）"
  fi
}

# ---------------------------- 包安装 ----------------------------
pkg_install() {
  case "$PKG" in
    apt-get)
      DEBIAN_FRONTEND=noninteractive apt-get update -qq || warn "apt-get update 有报错，继续"
      DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$@" ;;
    dnf) dnf install -y -q "$@" ;;
    yum) yum install -y -q "$@" ;;
    apk) apk add --no-cache "$@" ;;
    zypper) zypper -q install -y "$@" ;;
  esac
}

install_base_deps() {
  step "安装基础依赖"
  [ "$CHECK_ONLY" = "1" ] && { info "将安装：curl tar gzip unzip ca-certificates procps"; return 0; }
  pkg_install curl tar gzip unzip ca-certificates procps >/dev/null 2>&1 || true
  for c in curl tar gzip unzip; do
    command -v "$c" >/dev/null 2>&1 || die "依赖 $c 安装失败，请手动安装后重试"
  done
  ok "基础依赖就绪（curl / tar / gzip / unzip）"
}

install_openssl_if_possible() {
  command -v openssl >/dev/null 2>&1 && return 0
  [ "$CHECK_ONLY" = "1" ] && return 0
  pkg_install openssl >/dev/null 2>&1 || true
}

# ---------------------------- 随机串 ----------------------------
rand_hex() {
  n="${1:-16}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$n"
  else
    od -An -tx1 -N "$n" /dev/urandom | tr -d ' \n'
  fi
}

# ---------------------------- 官方 Core ----------------------------
install_hysteria() {
  [ "$WITH_HYSTERIA" = "1" ] || { warn "按参数要求跳过 Hysteria Core 安装"; return 0; }
  step "安装官方 Hysteria 2 Core"

  if command -v hysteria >/dev/null 2>&1; then
    ok "已安装：$(hysteria version 2>/dev/null | head -1 || echo '已存在')"
    return 0
  fi
  if [ "$CHECK_ONLY" = "1" ]; then
    info "将下载 https://github.com/apernet/hysteria/releases/latest/download/hysteria-linux-$ARCH"
    info "并安装到 $BIN_PATH"
    return 0
  fi

  tmp="$(mktemp -d)"
  url="https://github.com/apernet/hysteria/releases/latest/download/hysteria-linux-$ARCH"
  info "下载：$url"
  curl -fL --retry 3 --connect-timeout 20 -o "$tmp/hysteria" "$url" \
    || die "下载 Hysteria Core 失败（网络受限可手动放到 $BIN_PATH 后重跑）"
  install -m 0755 "$tmp/hysteria" "$BIN_PATH"
  rm -rf "$tmp"
  ok "已安装：$("$BIN_PATH" version 2>/dev/null | head -1 || echo "$BIN_PATH")"
}

# ---------------------------- 目录与用户 ----------------------------
setup_dirs() {
  step "建立目录与运行用户"
  [ "$CHECK_ONLY" = "1" ] && { info "将建立 $APP_DIR/{config,data,backup,web/dist,acme} 与用户 $RUN_USER"; return 0; }

  if ! id "$RUN_USER" >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
      useradd -r -M -s /usr/sbin/nologin "$RUN_USER" 2>/dev/null \
        || useradd -r -M -s /sbin/nologin "$RUN_USER" 2>/dev/null \
        || warn "创建用户 $RUN_USER 失败（继续，用 root 跑）"
    fi
  fi

  for d in config data backup acme templates web/dist; do
    mkdir -p "$APP_DIR/$d"
  done
  chown -R "$RUN_USER:$RUN_USER" "$APP_DIR" 2>/dev/null || true
  chmod 700 "$APP_DIR"/config "$APP_DIR"/data "$APP_DIR"/backup 2>/dev/null || true
  ok "目录就绪：$APP_DIR"
}

# ---------------------------- Panel 二进制 ----------------------------
install_panel() {
  step "部署 HY2 Panel"
  target="$APP_DIR/hy2-panel"
  # 没指定来源时：优先用仓库发布产物，其次现场编译
  if [ -z "$PANEL_BIN" ] && [ -z "$PANEL_URL" ] && [ ! -f "$PWD/hy2-panel-linux-$ARCH" ]; then
    PANEL_URL="https://github.com/$REPO/releases/latest/download/hy2-panel-linux-$ARCH"
  fi

  if [ -n "$PANEL_BIN" ] && [ -f "$PANEL_BIN" ]; then
    [ "$CHECK_ONLY" = "1" ] && { info "将使用本地文件：$PANEL_BIN"; return 0; }
    install -m 0755 "$PANEL_BIN" "$target"
    ok "已安装本地二进制：$PANEL_BIN"
  elif [ -n "$PANEL_URL" ] || { [ -n "$PANEL_BIN" ] && [ "${PANEL_BIN#http}" != "$PANEL_BIN" ]; }; then
    url="$PANEL_URL"; [ -n "$url" ] || url="$PANEL_BIN"
    [ "$CHECK_ONLY" = "1" ] && { info "将从 $url 下载 Panel 二进制"; return 0; }
    tmp="$(mktemp -d)"
    curl -fL --retry 3 --connect-timeout 20 -o "$tmp/hy2-panel" "$url" || die "下载 Panel 失败：$url"
    install -m 0755 "$tmp/hy2-panel" "$target"
    rm -rf "$tmp"
    ok "已下载安装：$url"
  elif [ -f "$PWD/hy2-panel-linux-$ARCH" ]; then
    [ "$CHECK_ONLY" = "1" ] && { info "将使用当前目录的 hy2-panel-linux-$ARCH"; return 0; }
    install -m 0755 "$PWD/hy2-panel-linux-$ARCH" "$target"
    ok "已安装当前目录二进制（$ARCH）"
  elif command -v go >/dev/null 2>&1 && [ -f "$PWD/go.mod" ]; then
    [ "$CHECK_ONLY" = "1" ] && { info "将现场 go build 编译"; return 0; }
    info "检测到 Go 与源码，开始编译（可能要几分钟）…"
    ( cd "$PWD" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -o "$target" ./cmd/hy2-panel ) \
      || die "编译失败"
    ok "已编译安装"
  else
    if [ "$CHECK_ONLY" = "1" ]; then
      info "未找到现成二进制 — 正式安装请用 --panel-bin <文件> / --panel-url <链接>，"
      info "或在本项目目录（带 go.mod）里运行，脚本会用 go 现场编译"
      return 0
    fi
    die "找不到 Panel 二进制：请用 --panel-bin ./hy2-panel-linux-$ARCH（或 --panel-url <链接>，或在本项目目录里跑）"
  fi
  chmod 0755 "$target" 2>/dev/null || true
}

install_web() {
  step "部署前端静态资源"
  src=""
  if [ -z "$WEB_DIR" ] && [ -z "$WEB_TAR" ] && [ ! -d "$PWD/web/dist" ]; then
    WEB_TAR="https://github.com/$REPO/releases/latest/download/hy2-web.tar.gz"
  fi
  if [ -n "$WEB_DIR" ] && [ -d "$WEB_DIR" ]; then src="$WEB_DIR";
  elif [ -d "$PWD/web/dist" ]; then src="$PWD/web/dist"; fi

  if [ -n "$src" ]; then
    [ "$CHECK_ONLY" = "1" ] && { info "将从 $src 拷贝前端资源"; return 0; }
    rm -rf "$APP_DIR/web/dist"
    mkdir -p "$APP_DIR/web/dist"
    cp -R "$src/." "$APP_DIR/web/dist/"
    ok "前端资源已部署：$APP_DIR/web/dist"
  elif [ -n "$WEB_TAR" ]; then
    [ "$CHECK_ONLY" = "1" ] && { info "将从 $WEB_TAR 解包前端资源"; return 0; }
    tmp="$(mktemp -d)"
    case "$WEB_TAR" in
      http*) curl -fL --retry 3 -o "$tmp/web.tgz" "$WEB_TAR" || { warn "下载前端包失败：$WEB_TAR"; return 0; } ;;
      *) cp "$WEB_TAR" "$tmp/web.tgz" ;;
    esac
    mkdir -p "$APP_DIR/web/dist"
    tar -xzf "$tmp/web.tgz" -C "$APP_DIR/web/dist" || die "解包前端资源失败"
    rm -rf "$tmp"
    ok "前端资源已解包"
  else
    warn "没找到前端资源（--web-dir / --web-tar）—— Panel 会启动但打不开界面"
    info "之后把 dist 放到 $APP_DIR/web/dist 再 systemctl restart $UNIT_PANEL 即可"
  fi

  if [ -d "$PWD/web/public/templates" ]; then
    [ "$CHECK_ONLY" = "1" ] || cp -R "$PWD/web/public/templates/." "$APP_DIR/templates/" 2>/dev/null || true
  fi
  [ "$CHECK_ONLY" = "1" ] || chown -R "$RUN_USER:$RUN_USER" "$APP_DIR" 2>/dev/null || true
}

# ---------------------------- 配置 ----------------------------
write_config() {
  step "生成本地配置"
  cfg="$APP_DIR/config/hysteria.yaml"

  if [ -s "$cfg" ]; then
    ok "已存在配置，保留不动：$cfg"
    return 0
  fi
  if [ "$CHECK_ONLY" = "1" ]; then
    info "将写入 $cfg（listen :$HY2_PORT，随机 obfs 密码 / 统计密钥）"
    [ -n "$DOMAIN" ] && info "并写入 ACME 块（$DOMAIN / $EMAIL）"
    return 0
  fi

  OBFS_PASS="$(rand_hex 8)"
  STATS_SECRET="$(rand_hex 32)"

  {
    echo "listen: :$HY2_PORT"
    if [ -n "$DOMAIN" ]; then
      echo "acme:"
      echo "  domains:"
      echo "    - $DOMAIN"
      [ -n "$EMAIL" ] && echo "  email: $EMAIL"
      echo "  ca: letsencrypt"
      echo "  type: http"
      echo "  http:"
      echo "    altPort: $ACME_ALT_PORT"
    fi
    echo "obfs:"
    echo "  type: salamander"
    echo "  salamander:"
    echo "    password: $OBFS_PASS"
    echo "quic:"
    echo "  initStreamReceiveWindow: 8388608"
    echo "  maxStreamReceiveWindow: 8388608"
    echo "  initConnReceiveWindow: 20971520"
    echo "  maxConnReceiveWindow: 20971520"
    echo "  maxIdleTimeout: 30s"
    echo "  maxIncomingStreams: 1024"
    echo "congestion:"
    echo "  type: bbr"
    echo "  bbrProfile: standard"
    echo "udpIdleTimeout: 62s"
    echo "auth:"
    echo "  type: userpass"
    echo "  userpass: {}"
    echo "outbounds:"
    echo "  - name: default"
    echo "    type: direct"
    echo "trafficStats:"
    echo "  listen: 127.0.0.1:9999"
    echo "  secret: $STATS_SECRET"
    echo "masquerade: {}"
  } > "$cfg"

  chown "$RUN_USER:$RUN_USER" "$cfg" 2>/dev/null || true
  chmod 600 "$cfg"
  [ -s "$cfg" ] || die "配置写入失败：$cfg"
  ok "已写入 $cfg"
  info "obfs 密码（客户端要用）：$OBFS_PASS"
}

# ---------------------------- systemd 单元 ----------------------------
write_units() {
  step "写入 systemd 单元"
  if [ "$SYSTEMD" != "1" ]; then
    warn "无 systemd，跳过（配置文件已放在 $APP_DIR）"
    return 0
  fi
  if [ "$CHECK_ONLY" = "1" ]; then
    info "将写入 /etc/systemd/system/$UNIT_HY.service 与 /etc/systemd/system/$UNIT_PANEL.service"
    return 0
  fi

  if [ "$WITH_HYSTERIA" = "1" ]; then
    cat > "/etc/systemd/system/$UNIT_HY.service" <<UNIT
[Unit]
Description=Hysteria 2 Server (official core)
Documentation=https://v2.hysteria.network/
After=network.target nss-lookup.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
ExecStart=$BIN_PATH server -c $APP_DIR/config/hysteria.yaml
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
    ok "已写入 $UNIT_HY.service"
  fi

  ADMIN_FLAG=""
  [ -n "$ADMIN_PASS" ] && ADMIN_FLAG=" -admin-password $ADMIN_PASS_UNIT"
  cat > "/etc/systemd/system/$UNIT_PANEL.service" <<UNIT
[Unit]
Description=HY2 Panel (Web management for the official Hysteria 2 server)
After=network.target$( [ "$WITH_HYSTERIA" = "1" ] && printf ' %s' "$UNIT_HY.service" )

[Service]
Type=simple
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/hy2-panel \\
    -listen :$PANEL_PORT \\
    -data-dir $APP_DIR/data \\
    -hy2-config $APP_DIR/config/hysteria.yaml \\
    -hy2-bin $BIN_PATH \\
    -backup-dir $APP_DIR/backup \\
    -service-unit $UNIT_HY \\
    -web-dir $APP_DIR/web/dist \\
    -initial-user $INITIAL_USER$ADMIN_FLAG
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=$APP_DIR $BIN_PATH

[Install]
WantedBy=multi-user.target
UNIT
  ok "已写入 $UNIT_PANEL.service"
  systemctl daemon-reload
  ok "systemd 已重载"
}

# ---------------------------- 防火墙 ----------------------------
setup_firewall() {
  [ "$WITH_FIREWALL" = "1" ] || { warn "按参数要求跳过防火墙"; return 0; }
  step "放行防火墙端口"
  ports="$PANEL_PORT/tcp $HY2_PORT/udp $HY2_PORT/tcp"
  [ -n "$DOMAIN" ] && ports="$ports $ACME_ALT_PORT/tcp"

  if [ "$CHECK_ONLY" = "1" ]; then info "将放行：$ports"; return 0; fi

  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi active; then
    for p in $ports; do ufw allow "$p" >/dev/null 2>&1 || true; done
    ok "ufw 已放行：$ports"
  elif command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    for p in $ports; do firewall-cmd --permanent --add-port="$p" >/dev/null 2>&1 || true; done
    firewall-cmd --reload >/dev/null 2>&1 || true
    ok "firewalld 已放行：$ports"
  else
    info "未检测到运行中的 ufw / firewalld（云厂商安全组请自行放行）"
  fi
}

enable_bbr() {
  [ "$WITH_BBR" = "1" ] || return 0
  step "开启 BBR 拥塞控制"
  cur="$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo unknown)"
  if [ "$cur" = "bbr" ]; then ok "已经是 bbr"; return 0; fi
  if ! sysctl -n net.ipv4.tcp_available_congestion_control 2>/dev/null | grep -q bbr; then
    warn "内核不支持 bbr，跳过"; return 0
  fi
  if [ "$CHECK_ONLY" = "1" ]; then info "将写入 /etc/sysctl.d/99-hy2-bbr.conf 并生效"; return 0; fi
  mkdir -p /etc/sysctl.d
  {
    echo "net.core.default_qdisc = fq"
    echo "net.ipv4.tcp_congestion_control = bbr"
  } > /etc/sysctl.d/99-hy2-bbr.conf
  sysctl --system >/dev/null 2>&1 || true
  ok "BBR 已开启（当前：$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)）"
}

# ---------------------------- 启动服务 ----------------------------
start_services() {
  step "启动服务"
  if [ "$SYSTEMD" != "1" ] || [ "$CHECK_ONLY" = "1" ]; then
    info "跳过（无 systemd 或 --check）"; return 0
  fi
  if [ "$WITH_HYSTERIA" = "1" ]; then
    systemctl enable --now "$UNIT_HY" >/dev/null 2>&1 || warn "$UNIT_HY 启动失败，看 journalctl -u $UNIT_HY"
  fi
  systemctl enable --now "$UNIT_PANEL" >/dev/null 2>&1 || warn "$UNIT_PANEL 启动失败，看 journalctl -u $UNIT_PANEL"
  sleep 2
  hy_state="$(systemctl is-active "$UNIT_HY" 2>/dev/null || true)"
  panel_state="$(systemctl is-active "$UNIT_PANEL" 2>/dev/null || true)"
  [ "$WITH_HYSTERIA" = "1" ] && info "$UNIT_HY：$hy_state"
  info "$UNIT_PANEL：$panel_state"
}

# ---------------------------- 收尾输出 ----------------------------
print_summary() {
  step "安装完成"

  ip="$(curl -4 -fsS --max-time 6 https://api.ipify.org 2>/dev/null || true)"
  [ -n "$ip" ] || ip="$(hostname -I 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i ~ /^[0-9]+\./) {print $i; exit}}')"
  [ -n "$ip" ] || ip="<服务器IP>"

  say ""
  say "    面板地址：  http://$ip:$PANEL_PORT"
  say "    管理员账号：$ADMIN_USER"
  if [ -n "$ADMIN_PASS" ]; then
    say "    管理员密码：$ADMIN_PASS"
  elif [ "$SYSTEMD" = "1" ]; then
    pw="$(journalctl -u "$UNIT_PANEL" -n 200 --no-pager 2>/dev/null | grep -iE '密码|password' | tail -2 | sed 's/^.*] //')"
    if [ -n "$pw" ]; then
      say "    初始密码：  $pw"
    else
      say "    初始密码：  看日志 -> journalctl -u $UNIT_PANEL -n 50"
    fi
  fi
  say ""
  say "    Hysteria 端口：$HY2_PORT/udp   （配置：$APP_DIR/config/hysteria.yaml）"
  say "    常用命令："
  say "      systemctl status $UNIT_PANEL $UNIT_HY"
  say "      journalctl -u $UNIT_PANEL -f"
  say "      systemctl restart $UNIT_PANEL"
  say ""
  [ "$WITH_HYSTERIA" = "1" ] && [ -z "$DOMAIN" ] && \
    info "没指定 --domain，证书未配置：可在面板「Hysteria2 配置 → TLS 与认证」里填域名与邮箱"
}

# ---------------------------- 卸载 ----------------------------
do_uninstall() {
  step "卸载 HY2 Panel"
  if [ "$SYSTEMD" = "1" ]; then
    systemctl disable --now "$UNIT_PANEL" >/dev/null 2>&1 || true
    systemctl disable --now "$UNIT_HY" >/dev/null 2>&1 || true
    rm -f "/etc/systemd/system/$UNIT_PANEL.service" "/etc/systemd/system/$UNIT_HY.service"
    systemctl daemon-reload
    ok "服务已停止并移除"
  fi
  if [ "$PURGE" = "1" ]; then
    if confirm "确认删除 $APP_DIR（含数据库、配置、证书）？"; then
      rm -rf "$APP_DIR"
      ok "已删除 $APP_DIR"
    fi
  else
    info "保留数据目录：$APP_DIR（加 --purge 可一并删除）"
  fi
  say ""
  say "    如需卸掉官方 Core：rm -f $BIN_PATH"
}

# ---------------------------- 主流程 ----------------------------
main() {
  say ""
  say "  HY2 Panel 一键安装"
  say "  ─────────────────────────────────────────────────────"
  detect
  [ "$UNINSTALL" = "1" ] && { do_uninstall; exit 0; }
  configure_admin

  install_base_deps
  install_openssl_if_possible
  install_hysteria
  setup_dirs
  install_panel
  install_web
  write_config
  write_units
  setup_firewall
  enable_bbr
  start_services

  if [ "$CHECK_ONLY" = "1" ]; then
    say ""
    ok "检查完成，没有改动系统（去掉 --check 即可真正安装）"
    exit 0
  fi
  print_summary
}

main "$@"
