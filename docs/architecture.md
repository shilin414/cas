# 当前架构与实现边界

核对日期：2026-09-21。入口为 `backend-go/cmd/*`、装配为 `internal/app`，前端路由为 `frontend/src/router`。

```text
浏览器 --HTTPS--> Docker TLS Nginx / Kubernetes Ingress
                           |
                       web Nginx
                    /      |      \\
              SPA/OCR   /api/...   /api/v2/runs/{id}/stream
                           |                  |
                         API                Stream
                           |                  |
                       MySQL 5.7 <------------+
                           |
                  Outbox -> Redis Streams -> 执行 Worker -> 飞书 Aily
                                             |
                                     结果 / Run / Artifact -> MySQL
                                             |
                              投递记录 -> 投递 Worker -> 飞书消息
Scheduler -> 计划触发 / 企业同步调度
Redis 同时承载 Session、缓存、限流与 Pub/Sub 通知
```

## 角色和正确性边界

- API 的业务写入、Run/Outbox 事实以 MySQL 为准；Redis 通知不是终态真相。
- 执行 Worker 通过 MySQL CAS + Lease/Heartbeat 获得执行权；Redis 消息重复不等于业务可以无条件重做。
- Stream 使用共享 SSE Hub 提供持续输出；反向代理必须关闭缓冲并允许长连接。
- 投递 Worker 与执行 Worker 是两种角色，同一个二进制用不同 `--provider` 启动。漏启动投递 Worker 会造成 pending 积压。
- Scheduler 同时运行自动化及企业目录同步调度；部署模板初始设一个副本，不宣称无需评估即可任意扩容。
- `cmd/migrate` 只应用待执行迁移并退出；`api -migrate` 会在迁移后继续运行 API，不应混入发布启动链。
- `cmd/seeddata` 是历史 Django 数据迁移工具；不是生产默认数据初始化，不在新镜像的运行入口中。

## 产品能力

Task 目前通过 Conversation 承载，Run 表示一次执行，Artifact 表示成果。工作台、自动化、目录同步、企业管理、业务应用、模型测试台均有独立代码。

当前执行注册表以飞书 Aily Agent 为主；不能把设计稿里的 Workflow、多 Provider 协作或界面占位标为已可运行。
业务应用走自己的受控上游接口，不自动进入 AI Run。模型测试台的独立测试记录不等于正式智能体已经切到这些模型。

## API 契约

`backend-go/api/openapi.yaml` 与生成代码覆盖主体契约；同步目标、业务应用、AI 模型等还应核对 HTTP 层的手工注册。不能仅凭 OpenAPI 推断所有真实路由。

## 数据与存储

- 当前迁移 0001—0044 是数据库结构来源。MySQL 5.7 方言，不假设 MySQL 8、TiDB 或 PostgreSQL 可直接替换。
- Redis 支持 standalone/Cluster；Redis 5 回收降级是项目代码实现，不是 Redis 自动提供。
- 头像、附件等二进制经 Storage 访问；`localfs` 需要各服务共享同一文件集合，或改为 S3 兼容存储。
- 加密密钥保护飞书凭据和模型连接凭据；恢复数据库时必须恢复匹配的密钥，不能每次部署重新生成。
- 生产 Secure Cookie + CSRF；同源部署是本文档默认，不承诺任意跨域配置可直接使用。

## 不要误解健康检查

API/Stream 的 `/health/live` 只代表 HTTP 服务可响应，`/health/ready` 检查 DB/Redis Ping。
它不验证数据库版本、Redis Slot 路由、存储写入、飞书权限、模型连接或 Worker 工作进度。
`/metrics` 在 8080/8081，`METRICS_ADDR` 虽有配置字段但没有单独启动 9090 服务。
