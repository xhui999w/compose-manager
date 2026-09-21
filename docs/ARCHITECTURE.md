# Compose Manager 系统架构

## 1. 总体结构

```text
Browser (React/Ant Design/Monaco)
             |
          HTTP JSON
             |
Go API ── application services ── SQLite
  |             |          |
Docker API   Compose CLI   guarded filesystem
  |             |          |
/var/run/docker.sock   allowed Compose roots + backups
```

生产镜像由多阶段构建生成：Node 阶段构建静态资源，Go 阶段编译后端，最终镜像只包含单个服务二进制、静态资源和 Docker CLI/Compose plugin。Go 服务同时提供 API 和前端静态文件。

## 2. 目录结构

```text
compose-manager/
├── backend/
│   ├── cmd/server/             # 进程入口
│   ├── internal/api/           # 路由、请求/响应、错误映射
│   ├── internal/app/           # 用例编排和 DTO
│   ├── internal/compose/       # 发现、路径守卫、CLI、编辑/备份
│   ├── internal/docker/        # Engine API adapter、统计、镜像引用
│   ├── internal/store/         # SQLite、迁移、设置/审计/历史
│   ├── internal/update/        # 策略、registry digest、更新流水线
│   └── web/                    # 构建时复制的前端 dist
├── frontend/src/
│   ├── app/                    # shell、router、主题
│   ├── api/                    # typed fetch client
│   ├── components/             # 通用紧凑控件
│   └── features/               # compose/containers/images/history/settings
├── docs/
├── data/
├── backups/
├── Dockerfile
└── compose.yaml
```

## 3. Docker Engine API

后端使用 Docker 官方 Go client，经 `DOCKER_HOST` 连接；默认 Unix Socket，兼容 Windows named pipe/TCP 配置。使用 API version negotiation。

读取接口：`Info`、`ContainerList`、`ContainerStats(one-shot)`、`ImageList`、`ImageInspectWithRaw`。写接口限定为容器 start/stop/restart、image pull/remove。前端只传对象 ID 和动作枚举；后端验证后调用 SDK，不接受命令字符串。

连接失败是可恢复状态：健康 API 返回服务可用但 Docker 状态 degraded，列表 API 返回稳定错误码 `DOCKER_UNAVAILABLE`。

## 4. Compose 项目发现

### 4.1 Label 发现

一次列出全部容器（包含停止容器），按 `com.docker.compose.project` 分组。收集：

- `com.docker.compose.project.config_files`
- `com.docker.compose.project.working_dir`
- `com.docker.compose.service`
- `com.docker.compose.container-number`

config_files 可能是逗号分隔多文件；只接受落在允许扫描根目录中的现存文件。路径可能来自宿主机而容器内挂载路径不同，因此通过配置的 host/container root 映射做显式转换，不猜测任意路径。

### 4.2 文件扫描

对配置根目录进行有界深度扫描，匹配四个标准文件名。忽略隐藏缓存、备份目录、`node_modules` 和配置的排除目录。使用目录名或 Compose 顶层 `name` 作为候选项目名。

### 4.3 合并

规范化真实路径（处理符号链接后）作为首要键，Compose project label 作为次要键。发现结果携带来源、置信度和能力：无文件路径的项目为 `labels-only`，不能编辑或运行 Compose CLI。

## 5. 资源统计

容器状态来自一次 `ContainerList`。运行容器的统计使用 Engine stats `stream=false` 并设置短超时；以有限 worker pool 并发获取。CPU 百分比按 Docker CLI 公式计算：

```text
cpuDelta = cpu.total_usage - precpu.total_usage
systemDelta = cpu.system_cpu_usage - precpu.system_cpu_usage
cpu% = cpuDelta / systemDelta * onlineCPUs * 100
```

内存使用为 `usage - inactive_file`（可用时），上限取 stats limit。项目统计为子容器求和。采样结果缓存 3–5 秒，避免每个前端请求重复访问 socket。

## 6. Compose CLI 与操作安全

必须使用 `exec.CommandContext("docker", args...)`，禁止 `sh -c`/`cmd /c`。允许动作与参数固定映射：

- start: `compose -f FILE --project-directory DIR up -d`
- stop: `compose ... stop`
- restart: `compose ... restart`
- pull: `compose ... pull [SERVICE]`
- config: `compose ... config --quiet`
- logs: `compose ... logs --no-color --tail N [SERVICE]`

项目名、服务名使用严格字符白名单；tail 有上下限；每个操作有超时、输出大小限制和项目级互斥锁。CLI 环境仅继承必要变量，不接收前端环境覆盖。

## 7. 安全文件编辑

`PathGuard` 保存启动时解析后的允许根目录。每次访问都做：绝对化 → 清理 → 解析现存父目录符号链接 → `filepath.Rel` 边界检查 → 标准文件名/扩展名检查。

保存使用乐观锁（原文件 SHA-256）。候选内容写入同目录权限受限的临时文件，以确保后续 rename 位于同一文件系统。基础 YAML 解析与 Compose CLI 校验通过后，先复制原文件到备份目录并写历史行，再使用 rename 原子替换。Windows rename 语义由文件适配器封装并测试。

Diff 使用行级 unified diff；前端 Monaco Diff Editor 只负责展示，最终 diff 和基准哈希由服务端生成。

## 8. SQLite

SQLite 保存设置、端口访问配置、Compose 版本元数据、更新记录和审计事件。启用 WAL、foreign keys、busy timeout，迁移在启动时事务执行。

核心表：

- `settings(key, value, updated_at)`
- `project_policies(project_key, service, mode, target_tag, updated_at)`
- `access_links(project_key, service, container_port, ...)`
- `compose_versions(id, project_key, file_path, sha256, backup_path, created_at)`
- `update_records(id, project_key, service, old_image, old_digest, new_image, new_digest, status, error, created_at)`
- `audit_events(id, action, target_type, target_id, result, detail, created_at)`

## 9. 镜像引用与删除

镜像引用图由三个来源构建：容器的 ImageID（区分运行/停止）、Compose YAML 的 service.image 字段、Docker image tags/digests。分类为规则输出，不持久化为真相。

删除 API 接收 image ID，不接收任意引用字符串。执行前重新拉取容器与 Compose 引用；任何引用存在即返回 `IMAGE_REFERENCED`。删除默认 `force=false`。可释放空间对共享 layer 只能估算，UI 明确标为预计值。

## 10. 更新检测

镜像引用解析为 registry/repository/tag。Registry client 先请求 manifest，处理 Bearer token challenge，再读取 `Docker-Content-Digest`；对 manifest list 记录列表 digest，并按本机平台解析子 manifest 作为扩展字段。Docker Hub 请求先读取 Docker Engine `/info` 中已经配置的 HTTPS registry mirrors，逐一回退后才直连官方仓库；只接受无用户信息、无路径前缀的 HTTPS mirror。凭据来自服务端 registry 配置，不下发前端。

- 后端启动后立即检查一次，此后由单一后台任务每 24 小时检查。
- 本地 RepoDigest 与远端 digest 不同即提示；缺少 Digest、认证失败或限流时保持 unknown。
- 自动任务只写入内存状态和审计事件，永不进入 pull/apply。
- Compose 项目与容器状态均由其引用镜像汇总；任一镜像存在更新即显示数量，全部已确认最新才显示“无更新”。
- pull/apply 只能由已登录用户在项目行上明确确认后触发。

检测任务限速、带退避并缓存结果。更新流水线串行锁定项目，所有阶段写 `update_records`。旧 ImageID/Digest 在 pull 前记录，为回滚保留。

## 11. RemoteAccessProvider

```go
type RemoteAccessProvider interface {
    ID() string
    Validate(context.Context, ProviderConfig) error
    Resolve(context.Context, AccessTarget) ([]AccessLink, error)
}
```

MVP 提供 `manual` provider：自定义 URL 或 domain/port/path。UGREENlink、Lucky、Cloudflare 以后作为独立 adapter 注入，不让核心服务依赖未公开 API。

## 12. API 概览

所有接口位于 `/api/v1`：

- `GET /health`
- `GET /auth/status`, `POST /auth/setup`, `POST /auth/login`, `POST /auth/logout`
- `GET /overview`
- `GET /compose/projects`, `POST /compose/projects/{key}/actions`
- `GET /compose/projects/{key}/logs`
- `GET|POST /compose/projects/{key}/file`, `POST .../validate`, `POST .../apply`
- `GET /compose/projects/{key}/versions`, `POST .../versions/{id}/restore`
- `GET /containers`, `POST /containers/{id}/actions`, `GET /containers/{id}/logs`
- `GET /images`, `DELETE /images/{id}`
- `GET /updates`, `POST /updates/run`
- `GET|PUT /settings`, `GET|PUT /access-links`

除 `/health` 和首次初始化/登录接口外，全部 API 由认证中间件保护。服务端仅在 SQLite 中保存会话令牌的 SHA-256 摘要；随机会话令牌通过 HttpOnly、SameSite=Strict Cookie 下发。写接口额外校验保存在会话记录中的 CSRF 令牌。HTTPS 部署通过 `CM_SECURE_COOKIE=true` 启用 Secure Cookie、`__Host-` Cookie 名和 HSTS；不根据未经信任的代理头自动判断 HTTPS。

管理员密码以 Argon2id PHC 格式保存，参数为 64 MiB、3 次迭代、并行度 2，每个密码使用独立随机盐。首次启动在 `/data/setup-token` 生成 192-bit 一次性初始化密钥并写入容器日志；创建唯一管理员后删除该文件并永久关闭初始化入口。登录失败使用单进程内按来源 IP + 用户名的时间窗限速。

## 13. 风险清单与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| Docker Socket 等同 root | 主机完全失陷 | 单独后端、最小 API、无任意命令、可信网部署、未来 socket proxy/rootless |
| label 中宿主路径与容器路径不一致 | 无法编辑/误定位 | 显式路径映射、根目录边界、不可验证则只读 |
| Compose CLI/Engine 版本差异 | 行为不一致 | 启动能力探测、最低版本提示、集成测试矩阵 |
| Registry API/认证/限流复杂 | 更新误报或失败 | digest 缓存、超时、退避、明确 unknown 状态、不自动升级关键服务 |
| YAML anchors/interpolation 多样 | 图形修改破坏语义 | 源码优先；快捷修改只做受限 AST 操作并展示 diff |
| 编辑与外部修改竞争 | 丢失更改 | SHA-256 乐观锁、原子替换、备份和历史 |
| 健康检查定义不统一 | 更新成功误判 | 区分 running/healthy/unknown，允许超时策略 |
| 镜像 layer 共享 | 可释放空间估算不准 | 标注预计值，删除后用 Engine 结果确认 |
| 单容器包含 Docker CLI 增大镜像 | 轻量性下降 | 多阶段构建、最小基础镜像、固定依赖版本 |
| 管理员凭据或会话泄露 | Docker 主机高权限被接管 | Argon2id、HttpOnly/Strict Cookie、会话摘要、CSRF、限速、CSP、HTTPS 与来源限制 |
| 首次初始化被抢占 | 攻击者先创建管理员 | 192-bit 一次性初始化密钥只保存在数据卷和容器日志，创建后立即失效 |
