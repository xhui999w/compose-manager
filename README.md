# Compose Manager

Compose Manager 是一个面向绿联 NAS、群晖 NAS 和 Linux Docker 主机的轻量级 Compose 管理面板。它将 Compose 项目总览、日常启停、日志、受限项目文件管理、可靠编辑、镜像引用检查和更新记录放在一个高密度桌面界面中。

> 当前状态：Phase 0–7 的可运行 MVP 骨架。真实 Docker 数据依赖宿主机 Docker Socket 与 `docker compose` CLI；在未连接 Docker 的开发环境中，健康检查仍可用，Docker 页面会显示明确的不可用状态。

## 设计原则

- Compose 是默认首页和一等模型。
- 一屏展示尽可能多的项目和容器，避免大卡片与装饰性图表。
- 前端永不直接访问 Docker Socket。
- 所有文件编辑先备份、先校验、后原子替换。
- 删除镜像前在服务端重新核对运行容器、停止容器与 Compose 文件引用。
- Docker/Compose 操作采用固定参数，不接受任意 shell 命令。

## 本地开发

要求：Go 1.23+、Node.js 22+、pnpm 11+。若要读取真实 Docker 数据，还需要 Docker Engine 与 Docker Compose v2。

```bash
# 后端
cd backend
go mod download
go run ./cmd/server

# 另一个终端：前端
cd frontend
pnpm install
pnpm dev
```

前端默认访问 `http://localhost:5173`，开发代理将 `/api` 转发到 `http://localhost:8080`。后端健康检查为 `GET /api/v1/health`。

## 单容器部署

```bash
docker compose up -d --build
```

## 从 GitHub Container Registry 安装（NAS 推荐）

每次推送到 `main` 后，GitHub Actions 会先运行前后端检查，再发布同时支持 `linux/amd64` 和 `linux/arm64` 的镜像：

```text
ghcr.io/<GitHub 用户名>/compose-manager:latest
```

在 NAS 上下载 `compose.registry.yaml`，并在同目录创建 `.env`：

```dotenv
CM_IMAGE=ghcr.io/<GitHub 用户名>/compose-manager:latest
CM_NAS_IP=192.168.1.10
CM_COMPOSE_PATH=/volume1/docker
CM_DATA_PATH=./data
CM_BACKUP_PATH=./backups
DOCKER_GID=0
CM_SECURE_COOKIE=false
```

然后启动：

```bash
docker compose -f compose.registry.yaml pull
docker compose -f compose.registry.yaml up -d
```

如果镜像包设为私有，NAS 需要先使用具备 `read:packages` 权限的 GitHub Personal Access Token 登录：

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u <GitHub 用户名> --password-stdin
```

公开镜像无需登录。正式版本可创建 `v1.0.0` 形式的 Git 标签，发布流程会额外生成对应版本镜像标签。

## 首次创建管理员账号

Compose Manager 默认保护除健康检查和认证以外的全部 API。首次启动后先查看一次性初始化密钥：

```bash
docker logs compose-manager 2>&1 | grep setupToken
```

打开面板，输入该密钥并创建管理员用户名和密码。密码要求 12–128 个字符。初始化成功后密钥立即失效，`/data/setup-token` 会被删除；后续进入面板必须登录。

密码使用 Argon2id 加随机盐保存，浏览器会话使用 HttpOnly、SameSite=Strict Cookie，写操作还必须提供与会话绑定的 CSRF 令牌。会话令牌不会写入浏览器本地存储。

若通过 HTTPS 反向代理访问，请设置 `CM_SECURE_COOKIE=true`；纯 HTTP 局域网部署保持 `false`，否则浏览器不会发送 Secure Cookie。不要把未启用 HTTPS 的管理面板直接暴露到公网。

默认挂载：

- `/var/run/docker.sock`（只由后端访问）
- `/data`（SQLite 与历史记录）
- `/backups`（Compose 文件备份）
- `/compose`（待扫描 Compose 目录；项目文件编辑与新建 Compose 需要读写挂载）

Docker Socket 等同宿主机高权限入口。即使已有内建登录，也应仅在可信局域网部署；需要公网访问时必须使用 HTTPS、来源限制和额外的反向代理防护。MVP 不提供多租户或企业 RBAC。

若容器内提示无权访问 Docker Socket，请将宿主机 Socket 的组 ID 传给 `DOCKER_GID`（例如 `stat -c '%g' /var/run/docker.sock` 的结果）后重建容器。

部分绿联 NAS 会将 `/volume1/docker` 内的目录和 Compose 文件限制为仅宿主机 root 可读；此时面板默认的非特权用户无法扫描这些项目。本机部署可在服务配置中设置 `user: "0:0"`，并在已有 `cap_drop: [ALL]` 下仅追加 `cap_add: [DAC_OVERRIDE, FOWNER]`。这会让面板进程能读取、修改挂载的 Docker 根目录，安全边界取决于面板登录、允许扫描的目录及网络访问控制；不要把面板直接暴露到公网。调整前请备份 Compose 配置，并确认 `/data`、`/backups` 对所选运行账号可写。

## 配置

Compose 首页每个项目的“更多”菜单可打开“删除项目”。页面先展示将移除的容器、Compose 文件及可选镜像和数据卷；确认时需输入项目名。后端执行前重新核对文件、Docker 标签与引用，并在 `/backups/deleted-projects/` 保存 Compose 配置。项目目录删除需单独勾选且仅允许独占的子目录；共享镜像、外部数据卷及其他项目使用的目录会保留。备份仅包含 Compose 配置，不包含应用数据或数据卷。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CM_LISTEN_ADDR` | `:8080` | HTTP 监听地址 |
| `CM_DATA_DIR` | `/data` | SQLite 数据目录 |
| `CM_BACKUP_DIR` | `/backups` | Compose 备份目录 |
| `CM_COMPOSE_ROOTS` | `/compose` | 逗号分隔的扫描根目录 |
| `CM_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker Engine 地址 |
| `CM_NAS_IP` | 自动推断 | 内网快捷访问主机 |
| `CM_DEFAULT_SCHEME` | `http` | 快捷访问默认协议 |
| `CM_UPDATE_CHECK_INTERVAL` | `24h` | 自动镜像更新检查周期；只检查，不自动升级 |
| `CM_DEMO_MODE` | `false` | 使用只读演示数据，便于界面预览 |
| `CM_SESSION_TTL` | `168h` | 登录会话有效期 |
| `CM_SECURE_COOKIE` | `false` | HTTPS 部署时设为 `true`，启用 Secure Cookie 与 HSTS |
| `CM_SETUP_TOKEN` | 自动生成 | 可选的首次初始化密钥；未设置时从容器日志读取 |
| `GOGC` | 镜像中为 `50` | 更积极回收 Go 临时内存，可按主机性能覆盖 |
| `GOMEMLIMIT` | 镜像中为 `96MiB` | Go 运行时软内存目标，不是容器硬限制，也不包含 Linux 文件缓存 |

## 内存策略

正式镜像不打包前端 source map；编辑器仍按需加载。项目列表、顶部状态栏和镜像列表共享最多 30 秒的目录发现快照，CPU / 内存采样共享最多 5 秒的结果并限制为 4 个工作协程。“刷新 / 重新发现”立即重扫；创建、保存、恢复、更新及删除会使快照失效。外部直接修改目录可通过刷新立即发现，普通读取最迟在快照过期后发现。

操作目标与删除前的容器、Compose 引用核对仍实时读取，不使用展示缓存。目录扫描分批读取，不会为了省内存跳过正常的嵌套项目。密码保留 Argon2id 的 64 MiB / 3 次计算强度，串行执行耗内存的计算并在完成后释放临时工作内存。登录、镜像更新和安全删除扫描仍可能短时提高内存，不能保证任何负载下固定 20 MiB；Docker 显示的容器内存与进程常驻内存应分开比较，不能通过修改统计口径制造下降。

## 文档

- [产品需求](docs/PRD.md)
- [系统架构](docs/ARCHITECTURE.md)
- [阶段任务与验收](docs/TASKS.md)
- [Compose 首页视觉基准](docs/design/compose-home-concept.png)

## MVP 边界

MVP 覆盖单机 Docker、Compose 项目发现与操作、受限 Compose 工作区与项目创建、源码编辑与恢复、镜像引用与安全删除判断、每 24 小时自动更新检查、用户确认更新、访问快捷方式和更新审计。应用商店、Swarm、Kubernetes、SSH/SFTP、根目录之外的通用文件管理、文件上传/删除/移动、Nginx/证书/DNS、高级网络/卷管理、多租户和企业 RBAC 明确不在范围内。

## License

尚未选择开源许可证；发布前必须补充。
