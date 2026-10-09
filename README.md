# HY2 Panel

官方 Hysteria 2 的轻量级 Web 管理面板 —— 一个二进制、一个 SQLite 文件，把「账号 /
订阅 / 日志 / 服务 / 证书 / 主机资源」全管起来。

后台是 Go（内嵌前端静态资源，无需 nginx），前端是 React + TypeScript，整套黑白
中性配色（浅色 / 深色两套），手机与桌面自适应。

---

## 一键安装

在**干净的 VPS**上用 root 执行（Debian / Ubuntu / CentOS / Rocky / Alma / Fedora / Alpine 均可）。

### 方式一：交互式（推荐，不把密码写在命令里）

```bash
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh
```

装到一半会问你账号密码：

```
==> 设置管理员账号
    直接回车用默认值；密码输入时不显示
    管理员用户名 [admin]: myadmin
    管理员密码（至少 8 位，留空则随机生成）:
    再输入一次确认:
    ✓ 管理员账号：myadmin（密码已设置）
```

> 交互输入是从 `/dev/tty` 读的，所以 `curl | sh` 这种管道写法也能正常输密码；
> 密码不回显，两次不一致或不足 8 位会让你重输，留空则由面板随机生成并在结束后打印。

### 方式二：参数直给

```bash
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s -- \
  --admin-user myadmin \
  --admin-password 'MyStrongPass123'
```

> 密码建议用单引号包住，避免 `$`、`!` 被 shell 吞掉。

### 方式三：完全免交互

```bash
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s -- -y
```

密码随机生成，装完在末尾打印。

### 脚本会自动完成

1. 识别发行版 / 架构（amd64・arm64・armv7・386）
2. 装基础依赖（curl / tar / unzip / ca-certificates …）
3. 从官方 release 装 **Hysteria 2 Core** 到 `/usr/local/bin/hysteria`
4. 建 `hy2-panel` 用户与 `/opt/hy2-panel` 目录树
5. 部署 panel 二进制（本项目 release 产物）与前端静态资源
6. 生成本地配置（随机 obfs 密码 与流量统计密钥）
7. 写入并启动两个 systemd 单元：`hy2-panel`、`hysteria-server`
8. 放行防火墙端口，可选开启 BBR
9. 打印面板地址与账号信息

装完访问 `http://<服务器IP>:8080` 登录即可。

### 常用安装参数

| 参数 | 说明 | 默认 |
|---|---|---|
| `--admin-user` | **管理员用户名** | 交互询问 |
| `--admin-password` | **管理员密码** | 交互询问 |
| `-y` / `--yes` | 全自动（密码随机生成） | — |
| `--port` | 面板端口 | `8080` |
| `--hy2-port` | Hysteria 监听端口 | `443` |
| `--domain` / `--email` | 直接写 ACME 域名与邮箱 | 空 |
| `--initial-user` | 首次启动自动建的 HY2 账号名 | `user1` |
| `--panel-bin` / `--panel-url` | 用指定二进制（本地路径 / URL） | 取 release |
| `--web-dir` / `--web-tar` | 用指定的前端资源 | 取 release |
| `--no-hysteria` / `--no-firewall` / `--no-bbr` | 跳过对应步骤 | — |
| `--check` | **只体检并打印计划，不改动系统** | — |

其它命令：

```bash
# 先看看它会做什么（不安装）
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/install.sh | sh -s -- --check

# 卸载（保留 /opt/hy2-panel 数据）
sh install.sh --uninstall

# 卸载并删数据
sh install.sh --uninstall --purge
```

---

## 升级更新

已经装了旧版的服务器，用 `upgrade.sh` 从 GitHub Release 重新拉取最新版并原地替换。
**只换二进制和前端，`config/` 与 `data/` 绝不改动**；旧版自动备份，启动失败自动回滚。

```bash
# 一步升级到最新版（推荐）
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/upgrade.sh | sh
```

常用用法：

```bash
# 只看有没有新版，不改动系统
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/upgrade.sh | sh -s -- --check

# 已经是最新，但仍强制重新拉取一遍
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/upgrade.sh | sh -s -- --force

# 升级后有问题，一键回滚到上一版
curl -fsSL https://raw.githubusercontent.com/xfgken/Hysteria2_Panel/main/upgrade.sh | sh -s -- --rollback

# 离线升级：把二进制和前端包先传到服务器，再本地跑
sh upgrade.sh --panel-bin ./hy2-panel-linux-amd64 --web-tar ./hy2-web.tar.gz
```

| 参数 | 说明 |
|---|---|
| `--check` | 只比对版本，不改动系统（会拉取小体积的前端包用于比对） |
| `--force` | 忽略版本比对，强制重新拉取覆盖 |
| `--rollback` | 回滚到上一次升级前的二进制与前端 |
| `--panel-bin F` | 用本地二进制升级，不走网络 |
| `--web-tar F` | 用本地前端包升级，不走网络 |
| `--no-web` | 只升级面板二进制，不动前端 |
| `--no-restart` | 只替换文件，不重启服务 |

升级过程做了这些事：

1. 从 `releases/latest` 按架构下载 `hy2-panel-linux-{amd64,arm64}` 与 `hy2-web.tar.gz`；
2. 校验下到的是合法 ELF 可执行文件（大小 + 魔数），不是就报错退出；
3. 比对前端资源名，与当前一致就提示「已经是最新版」并退出（不折腾服务）；
4. 备份：二进制存为 `hy2-panel.prev`，前端存为 `backup/upgrade-<时间戳>.tgz`；
5. 替换 `hy2-panel` 与 `web/dist`（同时更新 `templates/`）；
6. 重启服务并做健康检查（连本机端口，最多等 10 秒）；
7. 起不来 → 自动用备份回滚，并给出 `journalctl` 诊断命令。

默认位置可用环境变量覆盖：`HY2_APP_DIR`（默认 `/opt/hy2-panel`）、`HY2_UNIT`（默认 `hy2-panel`）。

## 功能

- **账号管理**：新增 / 修改 / 删除账号，名称前缀 `hysteria2-`，密码可随机生成
- **订阅输出**：每个账号一个订阅 Token，同时给 **官方 Hysteria2 URI** 与 **Clash Meta 订阅**（带二维码、一键复制）
- **连接日志**：后台轮询官方 `/online`，记录每个账号的「接入 / 断开」时间（终端风格展示）
- **VPS 主机监控**：CPU 使用率、负载、内存、交换、磁盘、运行时长 —— **每秒实时刷新**
- **服务控制**：面板内启动 / 重启 / 停止 Hysteria 服务，查看版本与状态
- **官方 Core 更新**：检查最新版、一键下载替换（先自检再原子替换，失败自动回滚）
- **配置管理**：基础 / TLS 与认证 / 传输与混淆 / 路由与出站 / 高级与备份
- **证书**：ACME 自动申请与续期，可查看证书信息
- **日志查看**：journal 或文件日志，级别筛选、行数切换、自动刷新
- **面板设置**：服务器地址、订阅基础地址、Clash mixed-port、日志来源
- **安全**：登录会话 + CSRF 校验，改端口自动放行防火墙

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go（标准库 `net/http`），SQLite（纯 Go 驱动，免 CGO），单二进制 |
| 前端 | React + TypeScript + Vite，自建 MD3 风格组件，黑白双主题 |
| 核心 | 官方 Hysteria 2（独立 systemd 单元，面板只管配置与上下线） |
| 部署 | `install.sh` 安装 + `upgrade.sh` 升级，两个 systemd 单元 |

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
upgrade.sh           一键升级脚本（从 Release 拉取最新版，可回滚）
deploy/              systemd / nginx 模板与示例配置
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
