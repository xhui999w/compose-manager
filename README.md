# Compose Manager

Compose Manager 是一个面向绿联 NAS、群晖 NAS 和 Linux Docker 主机的轻量级 Compose 管理面板。它将 Compose 项目总览、日常启停、日志、可靠编辑、镜像引用检查和更新记录放在一个高密度桌面界面中。

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

默认挂载：

- `/var/run/docker.sock`（只由后端访问）
- `/data`（SQLite、历史记录与会话签名密钥）
- `/backups`（Compose 文件备份）
- `/compose`（待扫描 Compose 目录，默认只读；若启用网页编辑需按需改成读写）

Docker Socket 等同宿主机高权限入口。请务必启用登录认证（见下节），并且只部署在可信局域网；MVP 不提供多租户或企业 RBAC。

若容器内提示无权访问 Docker Socket，请将宿主机 Socket 的组 ID 传给 `DOCKER_GID`（例如 `stat -c '%g' /var/run/docker.sock` 的结果）后重建容器。

## 登录认证

面板默认**不启用**登录——不设置口令时行为与旧版本一致，避免升级后被锁在门外。设置以下任意一个变量即可开启：

| 变量 | 说明 |
| --- | --- |
| `CM_AUTH_PASSWORD` | 明文口令，至少 8 个字符。服务端启动时即时转成 bcrypt 哈希，仅保存在内存。 |
| `CM_AUTH_PASSWORD_HASH` | bcrypt 哈希。与上一项同时配置时以哈希为准。**推荐**，可避免明文出现在 `.env`。 |

生成哈希（用镜像自带开关，避免把明文口令写进部署文件）：

```bash
docker run --rm ghcr.io/<GitHub 用户名>/compose-manager:latest -hash-password '你的口令'
```

把输出的 bcrypt 字符串填进 `.env`：

```dotenv
CM_AUTH_USER=admin
CM_AUTH_PASSWORD_HASH=$2a$12$....................................................
```

其余可选项：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CM_AUTH_USER` | `admin` | 登录用户名 |
| `CM_SESSION_SECRET` | 自动生成并落盘 | 会话 Cookie 的签名密钥。64 位十六进制或 ≥16 字节原始字符串。 |
| `CM_SESSION_SECRET_PATH` | `/data/.session-secret` | 未显式设置密钥时的落盘位置（0600） |
| `CM_SESSION_TTL` | `12h` | 登录有效期 |
| `CM_COOKIE_SECURE` | `false` | 通过 HTTPS 域名（如 Cloudflare 隧道）访问时建议设为 `true` |

行为说明：

- 会话是 HMAC-SHA256 签名的无状态 Cookie，服务端不存会话表。**容器重启不会掉登录态**（密钥复用 `/data/.session-secret`）。
- **登出只清除浏览器 Cookie**；若 token 已被复制，它在有效期内仍然可用。需要立即吊销全部会话时更换 `CM_SESSION_SECRET`。
- 登录失败按客户端 IP 限流：5 次失败后锁定 5 分钟。面板挂在反向代理后面时，所有请求共用同一个限流桶（服务端不信任 `X-Forwarded-For`）。
- `/api/v1/health` 保持匿名可访问，供容器健康检查使用；其余 `/api/*` 接口均需登录。


## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CM_LISTEN_ADDR` | `:8080` | HTTP 监听地址 |
| `CM_DATA_DIR` | `/data` | SQLite 数据目录 |
| `CM_BACKUP_DIR` | `/backups` | Compose 备份目录 |
| `CM_COMPOSE_ROOTS` | `/compose` | 逗号分隔的扫描根目录 |
| `CM_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker Engine 地址 |
| `CM_NAS_IP` | 自动推断 | 内网快捷访问主机 |
| `CM_DEFAULT_SCHEME` | `http` | 快捷访问默认协议 |
| `CM_DEMO_MODE` | `false` | 使用只读演示数据，便于界面预览 |
| `CM_AUTH_USER` | `admin` | 登录用户名 |
| `CM_AUTH_PASSWORD` | 空 | 明文口令（≥8 字符）；不填则不启用登录 |
| `CM_AUTH_PASSWORD_HASH` | 空 | bcrypt 哈希，优先于 `CM_AUTH_PASSWORD` |
| `CM_SESSION_SECRET` | 自动生成 | 会话 Cookie 签名密钥 |
| `CM_SESSION_SECRET_PATH` | `/data/.session-secret` | 自动生成密钥的落盘位置（0600） |
| `CM_SESSION_TTL` | `12h` | 登录有效期 |
| `CM_COOKIE_SECURE` | `false` | HTTPS 访问时建议设为 `true` |

登录认证的完整说明（含生成哈希与登出语义）见上文[登录认证](#登录认证)一节。

## 文档

- [产品需求](docs/PRD.md)
- [系统架构](docs/ARCHITECTURE.md)
- [阶段任务与验收](docs/TASKS.md)
- [Compose 首页视觉基准](docs/design/compose-home-concept.png)

## MVP 边界

MVP 覆盖单机 Docker、Compose 项目发现与操作、源码编辑与恢复、镜像引用与安全删除判断、三类更新策略、访问快捷方式和更新审计。应用商店、Swarm、Kubernetes、SSH/SFTP、文件管理、Nginx/证书/DNS、高级网络/卷管理、多租户和企业 RBAC 明确不在范围内。

## License

尚未选择开源许可证；发布前必须补充。
