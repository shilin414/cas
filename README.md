# 小安工作助手

企业 AI 工作台：以智能体/应用作为能力入口，以任务组织对话，以 Run 驱动执行并输出成果。当前运行代码是 **Go 后端 + React 前端**，不是 Django 项目。

> 文档核对日期：2026-09-21。以当前仓库源码、迁移和锁文件为准；历史整改报告不作为部署依据。此次部署模板尚未在目标 Docker/Kubernetes 环境完成上线验收。

## 当前技术与功能

- Go **1.27**（`backend-go/go.mod`）；React 18、TypeScript、Vite 7、Ant Design、Zustand（精确依赖以 npm 锁文件为准）。
- MySQL **5.7** 为持久化基线；仓库最新迁移为 **0045**（运行中心分页索引；本轮未执行）。Redis 支持 standalone / Cluster；Worker 对 Redis 5 使用 `XPENDING + XCLAIM` 回退。
- 认证为飞书 OAuth + 服务端 Session Cookie + CSRF；本地口令登录仅用于已有管理员账号，不依赖浏览器 JWT。
- 已有能力：任务工作台、飞书 Aily Agent、自动化计划及飞书投递、分享、企业目录同步、管理 RBAC/资源 ACL、业务应用、AI 模型管理/测试台与浏览器 OCR。
- **AI 测试台不等于已接入正式 Run 执行链**。协作流程设计、更多 Provider 不在当前已交付清单中；界面中有入口也不代表后端执行器已注册。
- API 路由查 `backend-go/api/openapi.yaml` **以及** `internal/transport/http` 的手工注册路由，不能只看 OpenAPI 文件。

## 六个常驻服务

| 服务 | 入口 / 端口 | 用途 |
| --- | --- | --- |
| API | `cmd/api` / 8080 | REST、认证、管理；开发代理也可走此 SSE 入口 |
| Stream | `cmd/stream` / 8081 | 独立 SSE 长连接 |
| 执行 Worker | `cmd/worker --provider=feishu_aily` | 执行 Run、Outbox Relay、回收 |
| 投递 Worker | `cmd/worker --provider=feishu_delivery` | 向飞书投递成果 |
| Scheduler | `cmd/scheduler` | 自动化与企业同步调度 |
| 前端 | 开发 Vite 3030；生产 Nginx | SPA、静态 OCR 资源、同源反向代理 |

迁移工具 `cmd/migrate` **不是常驻服务**。部署模板不运行 `-migrate`、数据库 InitContainer、Job 或 CI 生产迁移。

## 从这里开始

| 需求 | 文档 |
| --- | --- |
| 本地启动与验证 | [本地开发](docs/local-development.md) |
| dev / test / prod 配置 | [环境配置](docs/environment-profiles.md) |
| 架构与实现边界 | [当前架构](docs/architecture.md) |
| CentOS 7.9 / 运行环境限制 | [平台前置检查](docs/deployment/platform-prerequisites.md) |
| Docker 部署与升级 | [Docker 操作手册](docs/deployment/docker.md) |
| Kubernetes 1.34.1 / Kustomize 5.7.1 | [Kubernetes 操作手册](docs/deployment/kubernetes.md) |
| 人工建库、SQL 变更与管理员初始化 | [数据库操作手册](docs/deployment/database.md) |
| 健康检查、验收与回滚 | [运维手册](docs/operations.md) |
| 并发运行中心、权限与监控 | [运行中心](docs/operations-center.md) |
| 完整目录 / 清理说明 | [文档索引](docs/README.md) |

### 本机启动摘要

从 `backend-go` 目录为每个进程设置 `STUDIO_ENV=dev`，使用 `.env.development.local`；测试和生产分别使用 `.env.test.local`、`.env.production.local`。本机不再使用通用 `.env.local`，但加载器仍保留旧文件的回退逻辑，详见环境文档。

```powershell
# 在 backend-go 目录，先构建；各角色分别运行，不能只开 API。
$env:STUDIO_ENV = 'dev'
go build -o bin/api.exe ./cmd/api
go build -o bin/stream.exe ./cmd/stream
go build -o bin/worker.exe ./cmd/worker
go build -o bin/scheduler.exe ./cmd/scheduler
# 在独立终端分别执行：
.\bin\api.exe
.\bin\stream.exe
.\bin\worker.exe --provider=feishu_aily
.\bin\worker.exe --provider=feishu_delivery
.\bin\scheduler.exe
```

在 `frontend` 目录执行 `npm ci`、`npm run dev -- --port 3030 --strictPort`。
访问 `http://localhost:3030/xiaoan-platform/`，管理员入口为 `/xiaoan-platform/login/admin`。

## 目录

```text
backend-go/            Go 服务、SQL 迁移、测试
frontend/              SPA、OCR 资源准备、Nginx 配置生成器
deployment.json        公共基础路径
deploy/docker/         后端镜像、Compose、TLS Nginx 模板
deploy/k8s/            Kubernetes/Kustomize 模板
deploy/backend.env.example  无秘密的生产配置模板
docs/                  当前维护的操作与功能文档
```

## 安全与发布约束

- 真实 `.env.*.local`、密码、证书、导出的 Secret 不提交 Git、不打进镜像、不输出到 CI 日志。
- dev/test 当前按约定共用数据库和 Redis；**不是隔离测试环境**，不得直接跑破坏性集成测试。
- 更换 Redis 不会搬迁 Session/缓存/队列；更换数据库必须同时考虑加密密钥与文件存储，不能只改连接地址。
- 生产 Cookie 为 Secure，正式入口必须 HTTPS。上传内容需要共享持久化存储或 S3，不能留在 Pod 临时文件系统。
- 现有 Actions 的 push 分支是 `dev/main/exam`，并不包含 `test`；CI 内的临时测试库初始化不等同生产迁移。本次不改变 CI。
