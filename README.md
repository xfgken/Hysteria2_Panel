# HY2 Panel

官方 Hysteria 2 的轻量级 Web 管理面板 —— 一个二进制、一个 SQLite 文件，把「账号 /
订阅 / 日志 / 服务 / 证书 / 主机资源」全管起来。

后台是 Go（内嵌前端静态资源，无需 nginx），前端是 React + TypeScript，整套黑白
中性配色（浅色 / 深色两套），手机与桌面自适应。

---

## 一键安装

在**干净的 VPS**上用 root 执行（Debian / Ubuntu / CentOS / Rocky / Alma / Fedora / Alpine 均可）：

```bash
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s -- \
  --admin-user myadmin \
  --admin-password 'MyStrongPass123'
```

> 密码一定要改！建议带引号，避免 `$`、`!` 被 shell 吞掉。

脚本会自动完成：

1. 识别发行版 / 架构（amd64・arm64・armv7・386）
2. 装基础依赖（curl / tar / unzip / ca-certificates …）
3. 从官方 release 装 **Hysteria 2 Core** 到 `/usr/local/bin/hysteria`
4. 建 `hy2-panel` 用户与 `/opt/hy2-panel` 目录树
5. 部署 panel 二进制（仓库 release 产物）与前端静态资源
6. 生成本地配置（随机 obfs 密码 与流量统计密钥）
7. 写入并启动两个 systemd 单元：`hy2-panel`、`hysteria-server`
8. 放行防火墙端口，可选开启 BBR
9. 打印面板地址与初始密码

装完访问 `http://<服务器IP>:8080` 用刚才的账号密码登录即可。

### 常用安装参数

| 参数 | 说明 | 默认 |
|---|---|---|
| `--admin-user` | **管理员用户名** | `admin` |
| `--admin-password` | **管理员密码**（留空则随机生成并打印） | 随机 |
| `--port` | 面板端口 | `8080` |
| `--hy2-port` | Hysteria 监听端口 | `443` |
| `--domain` / `--email` | 直接写 ACME 域名与邮箱（也可登录后在面板里填） | 空 |
| `--initial-user` | 首次启动自动建的 HY2 账号名 | `user1` |
| `--panel-bin` / `--panel-url` | 用指定二进制（本地路径 / URL） | 取 release |
| `--web-dir` / `--web-tar` | 用指定的前端资源 | 取 release |
| `--no-hysteria` / `--no-firewall` / `--no-bbr` | 跳过对应步骤 | — |
| `--check` | **只体检并打印计划，不改动系统** | — |

其它命令：

```bash
# 先看看它会做什么（不安装）
curl -fsSL .../install.sh | sh -s -- --check

# 卸载（保留 /opt/hy2-panel 数据）
sh install.sh --uninstall

# 卸载并删数据
sh install.sh --uninstall --purge
```

---

## 功能

- **账号管理**：新增 / 修改 / 删除账号，名称前缀 `hysteria2-`，密码可随机生成
- **订阅输出**：每个账号一个订阅 Token，同时给 **官方 Hysteria2 URI** 与 **Clash Meta 订阅**（带二维码、一键复制）
- **连接日志**：后台轮询官方 `/online`，记录每个账号的「接入 / 断开」时间（终端风格展示）
- **VPS 主机监控**：CPU 使用率、负载、内存、交换、磁盘、运行时长——**每秒实时刷新**
- **服务控制**：面板内启动 / 重启 / 停止 Hysteria 服务，查看版本与状态
- **官方 Core 更新**：检查最新版、一键下载替换（先自检再原子替换，失败自动回滚）
- **配置管理**：基础 / TLS 与认证 / 传输与混淆 / 路由与出站 / 高级与备份，直接编辑官方 `config.yaml`
- **证书**：ACME 自动申请与续期，可查看证书信息
- **日志查看**：journal 或文件日志，级别筛选、行数切换、自动刷新
- **面板设置**：服务器地址、订阅基础地址、Clash mixed-port、日志来源
- **安全**：登录会话 + CSRF 校验，改端口自动放行防火墙

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go（标准库 `net/http`），SQLite（内嵌，纯 Go 驱动），单二进制 |
| 前端 | React 19 + TypeScript + Vite，自建 MD3 风格组件，黑白双主题 |
| 核心 | 官方 Hysteria 2（独立 systemd 单元，面板只管配置与上下线） |
| 部署 | 一个 `install.sh` + 两个 systemd 单元 |

## 目录结构

```
cmd/hy2-panel/       程序入口（flag、日志、启动）
internal/server/     HTTP 路由、接口、中间件、会话、采样器
internal/manager/    官方 Core 配置读写、校验、备份、服务操作
internal/hy2/        官方 Traffic Stats API 客户端
internal/db/         SQLite schema 与查询
internal/sub/        订阅生成（Hysteria2 URI / Clash Meta）
internal/firewall/   改端口时自动放行 ufw / firewalld / iptables / nftables
web/                 前端源码（web/dist 为已构建产物）
install.sh           一键安装脚本
```

## 从源码构建

```bash
# 后端（需要 Go 1.22+）
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o hy2-panel ./cmd/hy2-panel

# 前端（需要 Node 18+）
cd web && npm install && npm run build   # 产物在 web/dist
```

本地预览（不碰系统文件）：

```bash
ADMIN_USER=admin ADMIN_PASSWORD=admin123 bash scripts/preview.sh 8080
```

## 常用运维命令

```bash
systemctl status hy2-panel hysteria-server
journalctl -u hy2-panel -f
systemctl restart hy2-panel
```

## 注意

- 面板默认监听 `8080` **明文 HTTP**，公网使用请务必用强密码，或自己在前面加一层带 TLS 的反向代理
- `install.sh` **不会覆盖已有的 `/opt/hy2-panel/config/hysteria.yaml`**，重复执行是安全的（可用于修复安装）
- 云服务器的安全组需自行放行面板端口与 Hysteria 端口

## 致谢

- [Hysteria 2](https://v2.hysteria.network/) —— 核心协议与服务端
- 图标来自 Google Material Icons（Apache-2.0）
