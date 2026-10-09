#!/usr/bin/env bash
#
# HY2 Panel 一键安装脚本
#
# 对齐开发文档 59 / 60 节流程：
#   检查系统 → 检查 Root → 检查架构 → 检查网络 → 创建用户 → 创建目录
#   → 安装官方 HY2 Core → 安装 Panel → 初始化配置 → 创建 systemd
#   → 启动服务 → 检查服务 → 输出访问地址
#
# 用法：
#   bash install.sh
#
# 可选环境变量：
#   HY2_PANEL_BIN    本地 hy2-panel 二进制路径（优先使用）
#   HY2_PANEL_URL    Panel 二进制下载地址
#   HY2_CORE_URL     官方 Core 直链（默认使用官方安装脚本 get.hy2.sh）
#   PANEL_PORT       Panel 监听端口，默认 8080
#   INSTALL_NGINX    1 表示顺带安装 Nginx
#
# 注意：脚本不会写死任何默认密码；管理员密码由 Panel 首次启动时随机生成并打印。

set -euo pipefail

PANEL_DIR=/opt/hy2-panel
CONFIG_DIR="$PANEL_DIR/config"
BACKUP_DIR="$PANEL_DIR/backup"
DATA_DIR="$PANEL_DIR/data"
TEMPLATE_DIR="$PANEL_DIR/templates"
WEB_DIR="$PANEL_DIR/web/dist"
SERVICE_USER=hy2-panel
HY2_UNIT=hysteria-server
PANEL_UNIT=hy2-panel
PANEL_PORT="${PANEL_PORT:-8080}"
PANEL_BIN="$PANEL_DIR/hy2-panel"
CORE_BIN=/usr/local/bin/hysteria

info()  { printf '\033[32m==>\033[0m %s\n' "$1"; }
warn()  { printf '\033[33m[!]\033[0m %s\n' "$1"; }
fail()  { printf '\033[31m[x]\033[0m %s\n' "$1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. 前置检查
# ---------------------------------------------------------------------------
[ "$(id -u)" -eq 0 ] || fail "请以 root 运行（sudo bash install.sh）"

info "检查系统与依赖"
if [ -f /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  echo "    系统：${PRETTY_NAME:-unknown}"
fi

for cmd in curl tar systemctl; do
  command -v "$cmd" >/dev/null 2>&1 || fail "缺少必要命令：$cmd"
done

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   GOARCH=amd64 ;;
  aarch64|arm64)  GOARCH=arm64 ;;
  armv7l|armv7)   GOARCH=arm ;;
  *) fail "不支持的 CPU 架构：$ARCH" ;;
esac
echo "    架构：$ARCH（$GOARCH）"

info "检查网络连通性"
curl -fsSL --max-time 10 -o /dev/null https://api.github.com || warn "无法访问 api.github.com（更新检查将不可用）"

# ---------------------------------------------------------------------------
# 2. 创建用户与目录
# ---------------------------------------------------------------------------
info "创建运行用户与目录"
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --home "$PANEL_DIR" --shell /usr/sbin/nologin "$SERVICE_USER" 2>/dev/null \
    || useradd --system --home "$PANEL_DIR" "$SERVICE_USER"
fi

mkdir -p "$PANEL_DIR" "$CONFIG_DIR" "$BACKUP_DIR" "$DATA_DIR" "$TEMPLATE_DIR" "$WEB_DIR"
chmod 700 "$CONFIG_DIR" "$DATA_DIR" "$BACKUP_DIR"

# ---------------------------------------------------------------------------
# 3. 安装官方 Hysteria 2 Core
# ---------------------------------------------------------------------------
if [ -x "$CORE_BIN" ]; then
  info "已检测到官方 Core：$("$CORE_BIN" version 2>/dev/null | head -1 || echo 未知版本)"
else
  if [ -n "${HY2_CORE_URL:-}" ]; then
    info "从指定地址安装官方 Core"
    tmp="$(mktemp)"
    curl -fsSL --max-time 300 -o "$tmp" "$HY2_CORE_URL" || fail "下载官方 Core 失败"
    install -m 755 "$tmp" "$CORE_BIN"
    rm -f "$tmp"
  else
    info "使用官方安装脚本安装 Hysteria 2 Core"
    curl -fsSL --max-time 300 https://get.hy2.sh/ | bash || fail "官方安装脚本执行失败"
  fi
  [ -x "$CORE_BIN" ] || fail "官方 Core 未安装到 $CORE_BIN"
fi

# ---------------------------------------------------------------------------
# 4. 安装 Panel 二进制
# ---------------------------------------------------------------------------
info "安装 Panel 二进制"
if [ -n "${HY2_PANEL_BIN:-}" ] && [ -f "$HY2_PANEL_BIN" ]; then
  install -m 755 "$HY2_PANEL_BIN" "$PANEL_BIN"
elif [ -f "$(dirname "$0")/hy2-panel" ]; then
  install -m 755 "$(dirname "$0")/hy2-panel" "$PANEL_BIN"
elif [ -n "${HY2_PANEL_URL:-}" ]; then
  tmp="$(mktemp)"
  curl -fsSL --max-time 300 -o "$tmp" "$HY2_PANEL_URL" || fail "下载 Panel 失败"
  install -m 755 "$tmp" "$PANEL_BIN"
  rm -f "$tmp"
else
  fail "未提供 Panel 二进制：请设置 HY2_PANEL_BIN / HY2_PANEL_URL，或将 hy2-panel 放在脚本同目录"
fi

# 前端资源（可选：从发布包解压或已内置）
if [ -d "$(dirname "$0")/web/dist" ]; then
  info "安装前端静态资源"
  cp -r "$(dirname "$0")/web/dist/." "$WEB_DIR/"
fi

# 订阅模板
if [ -f "$(dirname "$0")/templates/clash-meta.yaml" ]; then
  cp "$(dirname "$0")/templates/clash-meta.yaml" "$TEMPLATE_DIR/clash-meta.yaml"
elif [ ! -f "$TEMPLATE_DIR/clash-meta.yaml" ]; then
  warn "未提供 Clash 订阅模板，订阅功能将不可用（可稍后放入 $TEMPLATE_DIR/clash-meta.yaml）"
fi

# ---------------------------------------------------------------------------
# 5. 生成官方 HY2 配置（若不存在）
# ---------------------------------------------------------------------------
if [ ! -f "$CONFIG_DIR/hysteria.yaml" ]; then
  info "生成自签名证书与初始配置"

  # 初始阶段通常还没有域名。官方 Core 在启动时会真实申请 ACME 证书，
  # 若用占位域名（如 example.com）会直接被 CA 拒绝并导致服务无法启动，
  # 因此这里先用官方自带的自签名证书，保证安装完成后服务立刻可用。
  HOST_HINT="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')"
  HOST_HINT="${HOST_HINT:-localhost}"
  if ! "$CORE_BIN" cert --cert "$CONFIG_DIR/server.crt" --key "$CONFIG_DIR/server.key" \
      --host "$HOST_HINT" --overwrite >/dev/null 2>&1; then
    warn "自签名证书生成失败，请手动配置 tls 或 acme"
  fi

  SECRET="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  cat > "$CONFIG_DIR/hysteria.yaml" <<YAML
# HY2 Panel 生成的初始配置
# 面板可在「服务器」页可视化编辑；修改前会自动备份。
listen: :443

# 初始使用自签名证书，服务可直接启动。
# 正式使用请改为真实域名 + ACME，或替换为正式证书：
#   acme:
#     domains: [your.domain.com]
#     email: your@email.com
#     type: http
#     http: { altPort: 80 }
tls:
  cert: ${CONFIG_DIR}/server.crt
  key: ${CONFIG_DIR}/server.key

auth:
  type: userpass
  userpass: {}

trafficStats:
  listen: 127.0.0.1:9999
  secret: ${SECRET}

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
  chmod 600 "$CONFIG_DIR/hysteria.yaml" "$CONFIG_DIR/server.key" 2>/dev/null || true
else
  info "已存在官方配置，保持不变：$CONFIG_DIR/hysteria.yaml"
fi

# ---------------------------------------------------------------------------
# 6. 创建 systemd 单元
# ---------------------------------------------------------------------------
info "写入 systemd 单元"

cat > "/etc/systemd/system/${HY2_UNIT}.service" <<UNIT
[Unit]
Description=Hysteria 2 Server (official core)
Documentation=https://v2.hysteria.network/
After=network.target nss-lookup.target

[Service]
Type=simple
WorkingDirectory=${PANEL_DIR}
ExecStart=${CORE_BIN} server -c ${CONFIG_DIR}/hysteria.yaml
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

# 端口跳跃 / 绑定低端口所需的能力
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

# 切勿限制 CapabilityBoundingSet：
# 一旦把 CAP_DAC_OVERRIDE / CAP_DAC_READ_SEARCH 排除在外，root 将无法读取
# 属主为 hy2-panel、权限 0600 的配置文件，服务会以
# "failed to read server config: permission denied" 反复重启失败。
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT

cat > "/etc/systemd/system/${PANEL_UNIT}.service" <<UNIT
[Unit]
Description=HY2 Panel (Web management for the official Hysteria 2 server)
After=network.target ${HY2_UNIT}.service

[Service]
Type=simple
# Panel 是「管理层」：需要通过 systemctl 启停官方服务，并在更新时替换官方二进制，
# 因此以 root 运行（只放开这两件事所需的能力，其余加固项保留）。
# 如需最小权限：可改用 polkit 规则授予 hy2-panel 管理 hysteria-server 的权限，
# 并把官方二进制放到 ${PANEL_DIR}/bin/ 下（位于 Panel 自己的可写目录内）。
WorkingDirectory=${PANEL_DIR}
ExecStart=${PANEL_BIN} \\
    -listen :${PANEL_PORT} \\
    -data-dir ${DATA_DIR} \\
    -hy2-config ${CONFIG_DIR}/hysteria.yaml \\
    -hy2-bin ${CORE_BIN} \\
    -backup-dir ${BACKUP_DIR} \\
    -service-unit ${HY2_UNIT} \\
    -web-dir ${WEB_DIR}
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
# /usr/local/bin 用于替换官方 Core；/opt/hy2-panel 用于配置、数据与备份
ReadWritePaths=${PANEL_DIR} /usr/local/bin

[Install]
WantedBy=multi-user.target
UNIT

chown -R "$SERVICE_USER:$SERVICE_USER" "$PANEL_DIR"
chmod 755 "$PANEL_BIN"

systemctl daemon-reload
systemctl enable "${HY2_UNIT}" "${PANEL_UNIT}" >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# 7. 可选安装 Nginx
# ---------------------------------------------------------------------------
if [ "${INSTALL_NGINX:-0}" = "1" ]; then
  info "安装 Nginx"
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -qq && apt-get install -y -qq nginx
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q nginx
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q nginx
  else
    warn "未知的包管理器，已跳过 Nginx 安装"
  fi
  systemctl enable nginx >/dev/null 2>&1 || true
fi

# ---------------------------------------------------------------------------
# 8. 启动并检查
# ---------------------------------------------------------------------------
info "启动服务"
systemctl start "${HY2_UNIT}"
systemctl start "${PANEL_UNIT}"
sleep 2

HY2_STATE="$(systemctl is-active ${HY2_UNIT} || true)"
PANEL_STATE="$(systemctl is-active ${PANEL_UNIT} || true)"

# ---------------------------------------------------------------------------
# 9. 输出结果
# ---------------------------------------------------------------------------
IP="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')"

# 启动横幅只写到标准输出（即 journald），因此优先从 journalctl 读取；
# panel.log 仅作为兜底（普通日志会写入该文件，横幅不会）。
PW="$(journalctl -u "${PANEL_UNIT}" --no-pager 2>/dev/null | grep -oP '管理员密码:\s+\K\S+' | tail -1 || true)"
if [ -z "$PW" ]; then
  PW="$(grep -oP '管理员密码:\s+\K\S+' "${DATA_DIR}/panel.log" 2>/dev/null | tail -1 || true)"
fi
HY2PW="$(journalctl -u "${PANEL_UNIT}" --no-pager 2>/dev/null | grep -oP 'HY2 用户密码:\s+\K\S+' | tail -1 || true)"

echo
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " HY2 Panel 安装完成"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " Panel 地址   http://${IP:-<服务器地址>}:${PANEL_PORT}"
echo " HY2 监听     UDP :443（见 ${CONFIG_DIR}/hysteria.yaml）"
echo " 官方服务     ${HY2_UNIT}: ${HY2_STATE}"
echo " Panel 服务   ${PANEL_UNIT}: ${PANEL_STATE}"
echo " 配置文件     ${CONFIG_DIR}/hysteria.yaml"
echo " 数据目录     ${DATA_DIR}"
if [ -n "$PW" ]; then
  echo
  echo " 面板管理员   admin"
  echo " 初始密码     ${PW}"
  echo " （密码仅首次启动时生成一次，请立即登录并修改）"
else
  echo
  echo " 管理员密码请执行以下命令查看："
  echo "   journalctl -u ${PANEL_UNIT} | grep 管理员密码"
fi
if [ -n "${HY2PW:-}" ]; then
  echo
  echo " HY2 初始用户 客户端连接用，密码：${HY2PW}"
  echo " （也可在面板「用户」页新增/删除用户）"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo
echo "常用命令："
echo "  hy2-panel status        查看官方服务状态"
echo "  hy2-panel update        更新官方 Core（失败自动回滚）"
echo "  hy2-panel backup        备份当前配置"
echo "  hy2-panel uninstall     卸载 Panel（默认保留数据）"
echo
warn "当前使用自签名证书（客户端需跳过证书校验）。正式使用请把域名与邮箱填入配置并改用 ACME，或替换为正式证书后在面板中保存应用。"
