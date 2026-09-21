# 三项功能联合发布说明

## 代码与依赖

已推送的自动化提交 `34192e0`（验证记录 `5608562`）保持不变。飞书预览卡片随后独立提交；部署子路径及跨功能验证在其后独立提交。后者使用卡片功能的共享 URL 配置，回退时应按依赖逆序处理，不要整文件覆盖共享配置或生成代码。

## 本次验证边界

本次仅整合代码、提交、推送与测试。没有迁移业务库、没有重启已有服务、没有启动飞书消费者，也没有发送真实飞书消息。网络验证使用独立端口、伪造的飞书/业务响应；SQL 验证使用连接级临时表或 CI 隔离数据库。

联合回归覆盖：条件不匹配时无卡片/分享/发送副作用；手动卡片只包含选中的消息；自动化网页仅包含当前执行结果与本次附件；复用会话的历史内容和标题不得泄漏；用户/群聊均为交互卡片；根路径、默认前缀及嵌套前缀的完整网页、附件、OAuth、API、SSE/WS 和静态资源。

## 后续使运行环境生效（以下未执行）

1. **安排维护窗口并备份。** 盘点现有调度/执行/投递进程及待发送队列。升级时不要混跑忽略推送条件的旧消费者；恢复投递前确认待处理历史记录，避免补发。
2. **统一配置。** 所有前后端进程使用相同 `deployment.json`，当前 `basePath` 为 `/xiaoan-platform/`。后端设置实际 `PUBLIC_ORIGIN`（仅协议、域名、可选端口）。独立安装时通过 `DEPLOYMENT_CONFIG` 指向该文件。高级 `APP_BASE_PATH` 覆盖必须同时用于前端构建、代理生成和所有后端进程。
3. **更新数据库。** 用现有 `cmd/migrate` 迁移入口应用尚未执行的版本，至少到 `0041`；`0040` 是自动化条件字段，`0041` 是结果分享的 nullable `source_run_id` 和唯一索引。不要在业务库手动运行测试脚本；迁移前后检查迁移记录及旧分享兼容性。
4. **重新构建。** 后端在 `backend-go` 执行 `go build ./...` 或现有发布构建；前端在 `frontend` 执行 `npm ci`、`npm run build`。`dist` 与生成的 Nginx 配置必须来自同一次路径配置。Docker 构建上下文是仓库根：`docker build -f frontend/Dockerfile -t xiaoan-platform-web .`。
5. **更新代理与服务。** 先检查新 Nginx 配置，然后发布 API/stream/执行 worker/调度器；确认 API 可解析新结果快照，再经人工确认恢复 `feishu_delivery` 消费者。生产 API 和投递 worker 必须使用兼容版本及同一个带前缀的 `PublicBaseURL`。不要直接拷贝已经变为说明文件的 `frontend/nginx.conf` 作为配置。
6. **更新飞书回调白名单。** 地址为 `${PUBLIC_ORIGIN}${basePath}auth/feishu/callback`。路径变化后重新登录，确认 Cookie Path、可读 CSRF Cookie 与同源 API 一致。真实授权与真实消息验收需要另行授权。
7. **上线后检查。** 检查用户/群聊卡片的完整网页链接、刷新深链、附件、SSE/WS及回调；公网 HTTPS / 非标准端口下，无尾斜杠入口应保持正确协议和端口。外层代理保留前缀，由生成的 Nginx 剥离一次。共享域名时合并本应用 location，不覆盖其他应用。

## 生成代码

从 `backend-go` 的最终 `api/openapi.yaml` 和 `db/queries` / `db/migrations` 运行项目固定版本的 `oapi-codegen -config api/gen.cfg.yaml api/openapi.yaml`、`sqlc generate`。不要手工合并生成字段或接口。

详细配置见 `docs/deployment-subpath.md`；联合审查与测试记录见 `.planning/2026-09-20-integrated-release/`。
