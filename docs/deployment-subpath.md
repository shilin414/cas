# 部署子路径、OAuth 与反向代理

## 路径来源

当前根目录 `deployment.json`：

```json
{"basePath":"/xiaoan-platform/"}
```

前端构建和 Nginx 生成器读取它；后端读取它，或使用显式 `APP_BASE_PATH`。本文模板固定当前路径，修改路径必须同时重新构建前端、更新 Compose/Kubernetes Nginx、Ingress 路径、后端变量和 Web 探针，不能只改其中一项。

路径只支持 `/` 或由字母数字、`_`、`-` 组成的多段目录；前后斜杠按规范归一化。

## 公网地址与回调

```dotenv
PUBLIC_ORIGIN=https://REPLACE_APP_DOMAIN
APP_BASE_PATH=/xiaoan-platform/
```

由当前后端 `configureDeployment` 最终构造：

- 公共根：`https://REPLACE_APP_DOMAIN/xiaoan-platform`
- OAuth 回调：`https://REPLACE_APP_DOMAIN/xiaoan-platform/auth/feishu/callback`
- Cookie Path：`/xiaoan-platform/`

飞书后台回调白名单必须登记最终完整 URL。单独修改旧 `FEISHU_REDIRECT_URI` 的 path 不能覆盖代码的路径拼接；明确配置 PUBLIC_ORIGIN 更可靠。生产 Secure Cookie 必须使用 HTTPS。

## 代理规则

| 浏览器请求 | 内部目标 |
| --- | --- |
| `/xiaoan-platform/` 与静态资源 | web Nginx SPA |
| `/xiaoan-platform/api/...` | `api:8080/api/...` |
| `/xiaoan-platform/api/v2/runs/{id}/stream` | `stream:8081/api/v2/runs/{id}/stream` |
| `/xiaoan-platform/ws` | 保留的 WS 代理规则；是否有业务处理器以当前后端路由为准 |

表中api/stream为Compose内部名称；Kubernetes模板使用带前缀的xiaoan-api/xiaoan-stream/xiaoan-web，避免覆盖共享namespace中的通用Service。

Ingress **保留整个路径**交给 web Nginx；web 只去除一次基础路径。不要再配置 Ingress strip/rewrite，否则路由错位。
SSE 禁用缓冲/缓存/压缩聚合并设置足够 read/idle timeout；当前 web SSE 模板 read timeout 为 3600s，上层 LB/Ingress 同样需要评估。

模型管理上传路由保留 21 MiB multipart 请求空间（文件上限20 MiB），其余路由20 MiB；Ingress/LB 上限不得更小。模型测试请求也可能持续较长时间，应同步调整对应 API 路由超时，不要只改 SSE。

## TLS 多层代理

Compose 模板由 web Nginx 直接终止 TLS。Kubernetes 由 Ingress 终止 TLS，web 使用转发的 `X-Forwarded-Proto`，避免把外部 HTTPS 改报为 HTTP。该信任只适用于**受控代理链**，web Service 不对公网开放，网关必须覆盖客户端伪造的转发头。
原 `frontend/scripts/deployment.mjs` 生成配置仍会写入自身 `$scheme`；本次模板通过挂载专用 Nginx 配置解决，不是修改该生成器。更换路径时同步维护这些文件。

健康检查使用后端内部 `/health/live` 与 `/health/ready`（无子路径）；无需将健康/metrics 暴露到公网。
