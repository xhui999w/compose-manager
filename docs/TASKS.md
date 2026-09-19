# Compose Manager 开发任务与验收

状态：`[x]` 已实现并通过当前环境可执行的验证；`[~]` 已实现但缺少本机外部运行时验证；`[ ]` 未完成。

## Phase 0 — 需求与架构

- [x] 初始化独立 Git 仓库与目录结构。
- [x] 创建 AGENTS、README、PRD、ARCHITECTURE、TASKS。
- [x] 明确 MVP、后续与不做范围。
- [x] 确定 Docker API、Compose 发现、资源统计、更新检测方案。
- [x] 记录 Docker Socket、路径映射、Registry、并发写入等风险。

验收：文档可以指导实现且无范围冲突；关键危险路径有服务端控制。

## Phase 1 — 可启动骨架

- [x] Go HTTP 服务、配置、健康检查、静态资源托管。
- [x] React + TypeScript + Vite + Ant Design 应用壳。
- [x] SQLite 初始化与迁移。
- [x] 多阶段 Dockerfile 与 compose.yaml。
- [x] 后端测试、前端 lint/build。

验收：开发模式可分别启动；生产容器为单个部署单元；无 Docker 时健康接口仍响应。

## Phase 2 — Docker 与 Compose 总览 API

- [~] Docker Socket 连接和能力状态（已实现；当前开发机无 Docker，待真实主机集成验证）。
- [x] Docker info、容器列表、CPU/RAM 一次性采样。
- [x] labels + 扫描目录 Compose 发现与合并。
- [x] `/api/v1/overview` 和 `/api/v1/compose/projects`。
- [x] 路径边界和发现逻辑测试。

验收：连接真实 Docker 后返回项目、子容器与聚合资源；路径逃逸被拒绝。

## Phase 3 — Compose 紧凑首页

- [x] 高密度 App Shell、系统状态条和项目表格。
- [x] 搜索、状态筛选、展开容器行。
- [x] 状态色、更新策略、内外网访问和行操作。
- [x] Docker 不可用、加载和空状态。
- [x] 1440×900 与 1024×768 浏览器视觉验证。

验收：桌面首屏接近 15–20 行容量；核心信息不依赖弹窗/多级页面。

## Phase 4 — Compose 操作与可靠编辑

- [x] 启动、停止、重启、日志固定参数 API。
- [x] 本地打包的 YAML/Monaco 编辑、搜索、行号、自动缩进（无 CDN 依赖）。
- [x] 服务端基础校验与 `docker compose config` 校验。
- [x] 服务端 Diff、备份、跨平台原子保存、仅保存/保存并应用。
- [x] 历史列表与恢复。
- [x] 操作互斥、超时、输出限制、审计和关键路径测试。

验收：无任意命令入口；校验失败不改变原文件；恢复前也生成备份。

## Phase 5 — 镜像管理

- [x] 镜像列表、tag/digest/size/time。
- [x] 运行/停止容器与 Compose 文件引用图。
- [~] 分类、预计可释放空间和筛选（基础分类已完成；“疑似更新残留”的 layer 级识别待真实 Docker 数据完善）。
- [x] 删除确认与执行前服务端二次检查。

验收：任何引用存在时默认拒绝删除；不提供自动清理。

## Phase 6 — 更新

- [~] latest、固定版本、仅检查策略模型（模式与展示已完成；固定版本的 tag 候选浏览/受控改写尚未完成）。
- [x] Registry manifest digest 检查、Bearer challenge 与 unknown 状态。
- [~] pull → up 流水线（已完成；基于 healthcheck 的等待/失败回退待真实主机集成验证）。
- [x] 旧/新镜像与 digest、结果、错误记录。
- [x] 更新记录页面。

验收：仅检查不执行变更；固定版本不自动改 tag；更新失败保留原 Compose 并留审计记录。

## Phase 7 — 访问与设置

- [x] 从端口映射生成内网 URL。
- [~] 端口显示名、路径、协议、快捷按钮设置（数据模型和默认 URL 已具备，逐端口编辑 UI 待补）。
- [~] 手动外网 URL/domain/port（设置持久化与 manual provider 已完成，逐项目绑定 UI 待补）。
- [x] `RemoteAccessProvider` 接口和 manual provider。
- [x] 设置页、紧凑度设置持久化与深/浅主题。

验收：URL 经过解析/转义；不依赖 UGREENlink 未公开 API；Provider 可替换扩展。

## Phase 8 — 登录认证

- [x] 应用内登录：`POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/session`。
- [x] bcrypt（cost 12）口令校验；支持明文口令与 `CM_AUTH_PASSWORD_HASH` 两种来源。
- [x] HMAC-SHA256 无状态会话 Cookie（HttpOnly + SameSite=Lax），服务端不存会话表。
- [x] 会话签名密钥自动生成并落盘（`/data/.session-secret`，0600）；容器重启不掉登录态。
- [x] 登录失败按客户端 IP 限流（5 次 / 5 分钟），只信任 TCP 对端地址、不信任 `X-Forwarded-For`。
- [x] `/api/*` 统一拦截，豁免 `/api/v1/health` 与三个 auth 接口；静态资源匿名可达以保证登录页可渲染。
- [x] 未配置口令时整体降级为不鉴权，保持既有部署升级后仍可访问。
- [x] 前端登录页、启动时会话探测、任意接口 401 统一退回登录页、侧边栏用户区与登出。
- [x] `-hash-password` 开关，便于在 NAS 上生成哈希而不留明文口令。
- [~] 容器内实际生效（镜像重建 + NAS 上 Docker Socket 挂载）待真实主机集成验证。

验收：未登录无法访问任何 `/api/*` 业务接口；登录后功能与改造前一致；不配置口令时行为与改造前完全一致。

## 发布前仍需

- [ ] 在 amd64 与 arm64 Linux + Docker Engine 上运行集成测试。
- [ ] 确认最低 Docker Engine/Compose v2 版本。
- [ ] 反向代理认证部署示例（应用内登录已实现，此项为可选加固，非必需）。
- [ ] 选择 License、版本号和镜像发布流程。
