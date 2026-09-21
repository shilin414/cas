# 可配置部署路径

## 唯一路径配置

仓库根目录 `deployment.json`：

```json
{
  "basePath": "/xiaoan-platform/"
}
```

以后修改为 `/studio/`、`/tools/studio/` 或 `/`，不需要替换业务代码。支持省略首尾斜杠，加载时统一补齐。只允许英文字母、数字、下划线、连字符和分隔斜杠；拒绝 `..`、查询参数、百分号编码及双斜杠。

这是**构建期配置，不是在线热切换**。变更后：

1. 重启 Vite（开发环境），或重新构建前端与 Nginx 配置并部署（生产环境）。
2. API、stream、所有 worker/scheduler 重启，读取相同的 `deployment.json`。
3. 在飞书开放平台把 OAuth 重定向 URL 更新为 `<PUBLIC_ORIGIN><basePath>auth/feishu/callback`。例如 `http://localhost:3030/xiaoan-platform/auth/feishu/callback`。这属于飞书平台配置，代码不能自动修改其白名单。
4. 更新监控、书签、外部集成和 E2E 脚本的浏览器入口。旧分享 URL 不会自动迁移；必要时在上层代理单独保留旧前缀重定向。

前端路由、JS/CSS 懒加载、favicon、axios、两类 SSE、WebSocket、登录跳转、分享、头像和附件 URL 均使用该前缀。React Router 的 `Link` / `navigate` 仍使用 `/chat/...` 等应用内路径，由 basename 加前缀，不能再手动拼一次。

## 域名与后端配置

在后端环境中设置：

```dotenv
PUBLIC_ORIGIN=https://example.com
```

只包含协议、域名和可选端口，**不带子路径**。后端自动派生：

- PublicBaseURL：`https://example.com/xiaoan-platform`
- 飞书 OAuth callback：`https://example.com/xiaoan-platform/auth/feishu/callback`
- 手动转发及自动投递卡片：`https://example.com/xiaoan-platform/share/<token>`
- Session / CSRF Cookie Path：`/xiaoan-platform/`

兼容已有配置：未设置 `PUBLIC_ORIGIN` 时，依次从 `PUBLIC_BASE_URL`、`FEISHU_REDIRECT_URI` 推导 origin，最后回落到 `http://localhost:3030`。旧 URL 的路径部分会被 `basePath` 替换，因此旧 `.env.local` 中的根路径回调不会覆盖新路径。建议迁移到 `PUBLIC_ORIGIN` 并移除旧 URL 配置，避免误解。生产环境必须设置实际公网 origin，不要使用 localhost。

后端默认从工作目录向父目录寻找 `deployment.json`。独立二进制 / 容器应挂载此文件并设置：

```dotenv
DEPLOYMENT_CONFIG=/etc/studio/deployment.json
```

找不到文件或配置非法会拒绝启动，而不是静默退回错误路径。高级部署可用 `APP_BASE_PATH` 覆盖，但**构建前端、生成 Nginx、运行所有后端进程时必须一致**。文件模式最不容易产生漂移。

`VITE_API_BASE_URL` 仍是可选高级覆盖，表示完整 API 基地址（含 `/api`），不是裸 origin。常规同域部署留空；跨域 API 还需独立设计 CORS、Cookie 和 CSRF，不在本次范围内。

## 开发

从 `frontend/` 执行 `npm run dev`，默认访问：

```text
http://localhost:3030/xiaoan-platform/
http://localhost:3030/xiaoan-platform/login/admin
```

Vite 将 `/xiaoan-platform/api/...` 转发为后端内部 `/api/...`；开发 SSE 仍走 API 进程，WebSocket 去掉部署前缀后转发。不要修改 Go 的 OpenAPI 路由 / 数据库存储路径，也不要给 React Router 内部路由重复添加前缀。

## Docker / Nginx

**构建上下文改为仓库根目录**，以便读取共享配置：

```sh
docker build -f frontend/Dockerfile -t xiaoan-platform-web .
```

Dockerfile 同次构建读取配置，生成 Nginx server 配置，把静态文件放到 `/usr/share/nginx/html/<basePath>/` 下。Nginx upstream `api:8080` 和 `stream:8081` 必须由部署网络解析。API / stream / workers 挂载同一个配置文件。

不用 Docker 时，在 `frontend/`：

```sh
npm run build
node scripts/deployment.mjs
```

把 `dist/` 的**内容**复制到 Nginx web root 的 `xiaoan-platform/` 目录（根路径部署直接放 web root），将 `nginx.generated.conf` 安装到 Nginx 的 `http` 上下文（如 `conf.d/default.conf`），执行 `nginx -t` 后 reload。`frontend/nginx.conf` 只是迁移提示，不再是可直接安装的配置。每次改前缀必须重新生成，不要手改生成文件。

生成配置的行为：

- 无尾斜杠入口 308 跳转到带斜杠入口，保留查询参数。
- SPA 深层页面刷新回退到前缀内 `index.html`；不存在的 `assets/` 返回 404，不回 HTML。
- 只有 `/api/v2/runs/<id>/stream` 进入 SSE gateway；Run detail/cancel 等 REST 仍进 API。
- SSE 关闭缓存和缓冲，保留长连接；WebSocket 保留 Upgrade。
- 向上游剥离一次前缀，保留 `/api`，透传 Host / Forwarded 信息。
- 子路径部署时本 server 的 `/` 返回 404，不抢占其他应用根路径。共用域名时把对应 location 合并到现有 server，而非覆盖其他应用规则。
- 上传上限为 20 MB；可按业务需要调整生成器中的代理上限。

若还有一层负载均衡：外层必须**保留前缀**转发给该 Nginx，否则会被二次剥离。生产 HTTPS 由外层终止时仍设置正确 PUBLIC_ORIGIN；APP_ENV=production 保持 Secure Cookie。

## Cookie 与迁移

新会话 / CSRF Cookie 均限于配置路径，登录与退出使用相同 Path。迁移登录时清理本项目旧的根路径 Cookie，避免同名 Cookie 优先级冲突。改前缀后可能需要重新登录，这是预期行为。不要让同域多个应用复用同名根 Cookie；需要多实例隔离时同时规划 Cookie 名称（前端 CSRF 名称需同步）、Redis key prefix 和 localStorage 命名空间，子路径本身不是完整租户隔离方案。

## 验证

```sh
# frontend/
npm run test:deployment
npm test
npm run build
# backend-go/
go test ./...
go vet ./...
```

部署专项测试覆盖默认、嵌套和根路径；使用真实 Vite HTTP 代理验证深链接、API、SSE 首帧和 WebSocket Upgrade。后端专项测试覆盖共享配置/环境覆盖、非法路径、Cookie 创建与退出、分享链接不受请求 Origin 干扰。

外部飞书授权需要先改白名单再做真实登录验证。本地测试不发送飞书消息、不跑数据库迁移，也不会重启其他任务的服务。

联合发布补充：卡片 URL 使用最终配置的带前缀 PublicBaseURL；Nginx 规范化跳转输出相对 Location，以保留外层 HTTPS/公网端口。CI 会从排除本机依赖、dist、生成配置及环境文件的根目录上下文构建镜像，并验证 nginx -t 与静态文件位置。三项功能的发布顺序与业务数据保护要求见 docs/integrated-release.md。
