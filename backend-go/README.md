# Go 后端

当前 Go 1.27，MySQL 5.7，Redis standalone/Cluster。主说明统一维护在根 README 与 docs，避免两套启动手册互相冲突。

- [项目入口](../README.md)
- [本地启动](../docs/local-development.md)
- [环境配置](../docs/environment-profiles.md)
- [当前架构](../docs/architecture.md)
- [人工 SQL 与迁移](../docs/deployment/database.md)
- [Docker](../docs/deployment/docker.md)
- [Kubernetes](../docs/deployment/kubernetes.md)

入口：`cmd/api`、`cmd/stream`、`cmd/worker`（执行/投递两角色）、`cmd/scheduler`。
`cmd/migrate` 仅人工维护时调用，不能默认随服务启动。`cmd/seeddata` 为历史迁移工具，不用于新生产库初始化。

结构来源：`db/migrations/*.up.sql`；当前最高版本0044。
配置实现：`internal/platform/config`；服务装配：`internal/app`。
主体 HTTP 契约：`api/openapi.yaml`，另核对 `internal/transport/http` 的手工注册扩展路由。

普通检查：`go build ./...`、`go vet ./...`、`go test ./... -count=1`。
带真实库的集成测试需独立隔离实例与环境守卫，不能运行在共用 dev/test 或生产数据库上。
