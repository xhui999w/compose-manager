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
- [x] 状态色、镜像更新状态和行操作；访问保留为行按钮，首页不再占用独立内/外网列或更新策略列。
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

- [x] 服务启动后立即检查、随后每 24 小时自动检查；无手动检查入口且不会自动升级。
- [x] 镜像检查结果按 Compose 文件和容器引用汇总到项目首页。
- [x] Registry manifest digest 检查、Bearer challenge 与 unknown 状态。
- [~] pull → up 流水线（已完成；基于 healthcheck 的等待/失败回退待真实主机集成验证）。
- [x] 旧/新镜像与 digest、结果、错误记录。
- [x] 更新记录页面。

验收：自动检查不执行 pull/apply；更新只在用户确认后执行；更新失败保留原 Compose 并留审计记录。

## Phase 7 — 访问与设置

- [x] 从端口映射生成内网 URL。
- [~] 端口显示名、路径、协议、快捷按钮设置（数据模型和默认 URL 已具备，逐端口编辑 UI 待补）。
- [~] 手动外网 URL/domain/port（设置持久化与 manual provider 已完成，逐项目绑定 UI 待补）。
- [x] `RemoteAccessProvider` 接口和 manual provider。
- [x] 设置页、紧凑度设置持久化与深/浅主题。

验收：URL 经过解析/转义；不依赖 UGREENlink 未公开 API；Provider 可替换扩展。

## Phase 8 — 单管理员认证与会话安全

- [x] 首次启动生成一次性初始化密钥并创建唯一管理员。
- [x] Argon2id 加盐密码哈希和恒定时间校验。
- [x] 服务端随机会话、数据库仅保存会话令牌摘要。
- [x] HttpOnly、SameSite=Strict Cookie；HTTPS 模式使用 Secure `__Host-` Cookie 与 HSTS。
- [x] 所有管理 API 强制登录，写 API 强制与会话绑定的 CSRF 令牌。
- [x] 登录失败限速、登录/初始化审计、退出立即撤销会话。
- [x] 创建账号/登录界面、当前用户名和退出入口。
- [x] 后端单元测试、API 中间件测试、真实 HTTP 端到端流程验证。

验收：匿名只能访问健康检查和认证入口；首次密钥使用后失效；密码不以明文或可逆形式保存；无 CSRF 的写请求被拒绝；退出后旧会话立即返回 401。

## 发布前仍需

- [ ] 在 amd64 与 arm64 Linux + Docker Engine 上运行集成测试。
- [ ] 确认最低 Docker Engine/Compose v2 版本。
- [ ] 增加 HTTPS 反向代理与可信来源限制部署示例。
- [ ] 选择 License、版本号和镜像发布流程。
