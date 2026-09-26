# 日志与运维可观测性审查报告

- 审查日期：2026-09-22。
- 基线：HEAD `aa6e4ad`，加当前工作区已有的未提交改动（含并发整改）。不是仅审查已发布版本。
- 方式：源码、部署配置、运维文档审查；运行 5 项现有定向单元测试。未连接生产环境，未读取真实密钥或完整业务日志，未执行有副作用的 DB/Redis 集成测试。
- 本次仅新增审查记录和本报告，没有修改业务代码或部署配置。

## 一、结论

**有基础，但不够完整：适合开发调试和人工排障，不宜据此认定已经满足长期生产运维需求。**

问题不是“日志行数太少”，而是几个关键链路没有形成可验证的闭环：

1. 用户看到错误，后端不一定留下可关联的错误日志。
2. 后台任务有指标，但现有 Worker/Scheduler 进程不提供采集出口。
3. request_id / run_id / worker_id / trace_id 没有连成一条可检索的链。
4. 个别异步失败和审计写入失败仍可能静默。
5. 仓库尚未交付集中采集、检索、告警路由、留存和故障演练的完整配置。

上述第 5 点是仓库交付物缺口，不代表实际部署环境一定没有外部监控平台；需要运维另行提供配置和演练证据。

### 场景判断

| 场景 | 判断 |
| --- | --- |
| 开发阶段看 stdout 定位常见错误 | 基本可用 |
| 小规模内部试运行，有开发人员值守并会查数据库 | 有条件可用，仍有盲区 |
| 跨 API/Worker/Scheduler、多副本自动定位故障 | 不足 |
| 无人值守，异常主动告警、升级后追溯、长期审计 | 尚不能验收通过 |

## 二、已有能力值得保留

- `internal/platform/logging/logging.go:20-33`：slog JSON stdout，支持 debug/info/warn/error，适合作为统一结构化日志基础。
- `internal/transport/http/middleware.go:155-164`：生成请求 ID 并通过 `X-Request-Id` 返回前端。
- `internal/platform/telemetry/telemetry.go`：已有 HTTP、Run、SSE Hub、容量、调度、投递、租约恢复和执行不变量等指标定义；其中多个关键指标已有实际更新点。
- `internal/execution/worker.go`、`internal/delivery/worker.go`、`internal/automation/scheduler/scheduler.go`：已有领取失败、失去所有权、重试、回收、投递失败、调度跳过等日志。
- Run 事件、投递状态和审计数据已有数据库持久化。它们是业务事实来源，不应因为增加运维日志而取消，也不能被当作集中日志系统的替代品。
- `internal/aimodel/audit.go:22-47`：AI 管理审计只记录字段名、版本等，避免原始凭证和配置内容；可作为其他模块的字段白名单参考。
- `deploy/docker/compose.yaml:25-27,67-69`：后端和前端容器已配置 `json-file`，每文件 20m、最多 5 个文件。不能说项目没有日志轮转。
- `docs/operations.md:24-29` 已明确提示 Worker/Scheduler 没有 HTTP 探针，也没有将 API Ready 误写成整个系统健康。

以下代码位置若没有特别说明，均相对于 `backend-go/`。

## 三、已确认问题与优先级

P1 = 正式上线验收前优先补齐；P2 = 紧随其后完善。这里不是在断言发生了生产事故。

### P1-1：HTTP 访问日志与内部错误记录缺位

**证据**
- `internal/transport/http/server.go:191-205` 的中间件链包含恢复、请求 ID、指标、认证和 CSRF，没有请求完成日志中间件。
- `internal/transport/http/errors.go:21-33` 的统一响应函数只写 JSON；`mapDomainError` 默认分支直接回传 `err.Error()`。
- `internal/transport/http/application_handlers.go:344-347` 等路径在内部调用失败后直接返回 500，没有在该错误边界记录服务端日志。

**影响**：用户报“服务器错误”后，应用日志未必能回答哪个操作失败、原始异常是什么、耗时多少；且部分路径把内部异常细节直接返回调用者。

**建议**：
- 统一请求完成事件：method、规范化 route、status、duration_ms、request_id，必要时增加经身份验证的 user_id。
- 在错误处理边界记录一次完整但脱敏的内部错误；外部只返回稳定错误码、安全文案和请求 ID。
- 不要在每一层重复打印相同异常；健康检查和高频成功请求降噪。
- 长连接单独记录连接建立/结束/原因，不能把 SSE 持续连接时间当作普通 API 响应延迟。

### P1-2：Worker/Scheduler 指标没有采集出口

**证据**
- `internal/app/app.go:105-108` 每次构建创建一个进程内指标注册表；`internal/platform/telemetry/telemetry.go:261-266` 使用独立 Registry。
- `cmd/worker/main.go`、`cmd/scheduler/main.go` 没有启动 HTTP 指标服务；API/Stream 的 `/metrics` 只服务各自进程的 Registry。
- `internal/execution/finalize.go:170-178` 等在后台进程更新 Run 指标，投递和调度同样在各自进程执行。
- `deploy/k8s/base/workloads.yaml` 没有为 Worker/Scheduler 配置指标出口；`docs/operations.md:26-28` 确认其没有 HTTP 服务。

**影响**：只采 API/Stream 时，后台任务吞吐、失败、积压和容量等指标无法因此自动出现在监控系统。指标定义再丰富也不能替代进程级采集。

**建议**：为各常驻后端角色提供受内网保护的指标端点及采集配置；增加最后成功消费/调度时间、最老待处理年龄和检查失败信号。不能只看进程 Running，也不能仅凭一条启动日志判断健康。

### P1-3：关联字段及分布式追踪没有贯通

**证据**
- `internal/transport/http/middleware.go:155-163` 把 logger 写入请求 context；生产代码中 `logging.FromContext` 的调用集中在同文件，业务普遍使用预先注入的 `s.Log`、`e.Log`、`w.Log`。
- `cmd/worker/main.go:40-42` 先设置默认 logger，再生成带 worker_id 的子 logger；`app.Build` 读取的是默认 logger。后续给 `a.Log` 赋值不会反向更新已经构造的组件。
- `internal/execution/outbox.go:80-84` 的队列消息只带 outbox_id、run_id、event，没有追踪传播字段。
- `internal/platform/telemetry/telemetry.go:557-601` 配置了 exporter、propagator 和 span helper，但检索未发现业务对该 helper 的调用，亦未发现 HTTP span 创建或上游注入/队列提取实现。
- 根日志没有统一附加 service/role/env/version/instance 等身份字段；trace_id 目前主要是键常量，而非已贯通的链路。

**影响**：一个任务跨 API、Outbox、执行 Worker、上游和投递 Worker 后，通常仍需人工用业务 ID 查库拼接；仅填写 OTEL 地址不能自动获得完整 trace。

**建议**：先贯通 request_id → run_id → occurrence_id/delivery_id，固定每类事件的必要关联字段；统一启动时生成角色 logger。再补真实 span、上游传播和队列追踪上下文。重试 attempt/lease_epoch 应明确记录，避免将旧 Worker 与新 Worker 的日志混为一次执行。长时间等待/重试可采用 span link，不必强行维持一个超长请求 span。

### P1-4：Panic 缺堆栈，还会漏记请求 ID 与 HTTP 指标

**证据**
- `internal/transport/http/server.go:192-194` 顺序是 `Recovery → RequestInfo → Metrics`。
- `internal/transport/http/middleware.go:173-176` 仅记录 panic 值和 path，没有 stack；外层 Recovery 持有的请求不包含内层新建的请求 logger。
- `internal/transport/http/middleware.go:190-195` 在 `next.ServeHTTP` 正常返回后记录指标，panic 展开时直接跳过。
- `internal/execution/worker.go:333-334,487-488`、`internal/delivery/worker.go:162-164` 同样没有堆栈。

**影响**：最需要定位的崩溃只留下简短文本；常见的响应前 panic 虽被恢复成 500，却缺失该次 HTTP 指标和请求关联。

**建议**：重新安排请求关联、访问日志/指标和恢复的包裹关系，使恢复后的状态被统一记录；补 stack、service/version、request_id/run_id、worker_id。补测试验证“请求 ID 一致、panic 仅记一次、500 指标增加、堆栈可见”，并保持 SSE Flush/Unwrap 能力。

### P1-5：Outbox 存在静默失败分支

**证据**
- `internal/execution/outbox.go:85-96`：XAdd 失败后只尝试写 last_error，且忽略 FailOutboxEvent 本身的错误，随后 continue。
- 该分支不会让 RunOnce 返回错误，因此不会触发 `:115-117` 的 tick 失败日志。
- `:120-122` 查询积压失败也被直接忽略。

**影响**：Redis 发布失败时，若 DB 的 last_error 写入也失败，两侧故障可能只体现为任务长时间等待；积压指标还可能停留在旧值。

**建议**：发布失败按错误类型聚合计数，周期性输出摘要（失败数量、最老年龄、代表性 outbox_id/run_id、脱敏错误）；持久化失败单独升级记录；指标采集失败有独立信号，不能把陈旧数值当成健康。成功发布不必逐条刷 INFO。

### P1-6：仓库未交付集中采集和主动告警闭环

**证据**
- Compose 已有限额轮转，但限额不是“保留多少天”；高流量时历史窗口会缩短。
- 维护中的 deploy、运维文档和 CI 配置中未找到可部署的日志采集器、Prometheus scrape/告警规则、告警通知路由及仪表盘配置。
- 独立的 `cmd/invariant-checker` 有 `/metrics`，但当前常驻服务部署模板未部署它，不能假定不变量检查已经在线。

**建议**：优先接入现有运维平台，不必为本项目重复搭建一套。交付角色/环境/实例标签、JSON 字段解析、内网采集、查询入口、告警责任人和恢复通知，并完成一次实际演练。平台托管的采集配置应在本项目记录引用和验收证据。

### P2-1：部分指标只有定义，部分口径不一致

**证据**
- QueueDepth、ProviderCalls、Provider429、DBLatency、RedisLatency 在生产 Go 文件中仅发现定义、创建、注册，未找到实际更新点。
- `internal/platform/telemetry/telemetry.go:53-57` 声明 DeliverySendsTotal 是外部发送尝试数；`internal/delivery/worker.go:208-221` 实际只在发送成功且完成状态 CAS 成功后累加。发送失败、外部已成功但本地落库失败都不会计入。
- `internal/platform/telemetry/metrics_test.go` 的部分测试手动 Set/Inc 后检查指标是否导出，验证了注册契约，但不证明生产路径更新和远端采集。

**建议**：建立“定义 → 更新位置 → 所属进程 → 采集目标 → 告警/仪表盘 → 场景测试”清单；先修真实业务更新点，再启用相应告警。分开发送尝试、上游确认成功、本地成功持久化三种事件。

### P2-2：认证审计写入失败完全不可见

**证据**
- `internal/identity/repo.go:242-253` 的 WriteAuditLog 忽略 CreateAuditLog 错误。
- `internal/transport/http/identity_handlers.go` 用它记录管理员登录成功与失败。
- 对比：RBAC/目录/AI 管理及敏感业务操作已经存在更严格的审计处理，不能笼统称“项目没有审计”。

**建议**：登录审计可以按既定策略不阻断登录，但审计失败必须有独立日志和计数；高风险管理修改继续使用事务或已存在的 fail-closed 策略。进一步核对操作人、操作对象、结果、时间、请求关联的字段完整性。

### P2-3：脱敏、留存和前端故障线索尚未统一

**证据与边界**
- `internal/platform/logging/logging.go` 没有统一脱敏/字段白名单机制。
- `internal/integrations/aily/client.go:77-105` 接收上游 Msg；`internal/integrations/aily/executor.go:420,1137-1139` 可将它直接写入日志。只能据此确认潜在风险，本次没有证据证明生产日志已泄露秘密。
- `frontend/src/services/axios.ts` 的响应拦截器对 500 提示“服务器错误”，未把 X-Request-Id 提取为用户可复制的排查编号；检索未发现全局异常上报设施。
- 业务会话删除会清理关联 run_events，但未发现独立、按时间执行的 audit_logs/已发布 outbox/run_events 归档与留存方案。不能把业务删除当成运维留存策略。

**建议**：
- 不记录 Cookie、Authorization、access/refresh token、API Key、原始提示词、聊天全文、附件正文或签名 URL；错误消息做长度限制和敏感内容处理。
- 检查网关访问日志是否会收集 OAuth code、ticket、分享密钥等查询参数，不只检查应用 JSON 日志。
- 日志、审计、对话/Run 事件分别制定保留策略与访问权限，清理前保留业务重放和追溯要求，不对业务表盲目加 TTL。
- 前端在反馈错误时可复制排查编号；异常上报只采必要且脱敏的上下文，不默认上传聊天内容。

## 四、建议补充日志的位置与内容

以下事件名为建议，不代表当前代码已经实现。

| 边界 | 建议事件 | 必需字段 | 级别与降噪 |
| --- | --- | --- | --- |
| 启动/退出 | service.started / stopping | service,role,env,version,instance,worker_id | INFO，不输出完整配置 |
| HTTP 完成/失败 | http.request.completed / failed | request_id,method,route,status,duration_ms,error_code | 5xx ERROR；普通4xx不机械升ERROR；成功按需采样 |
| Run 入队/领取/终态 | run.enqueued / claimed / finished | request_id,run_id,provider,worker_id,attempt,lease_epoch,status,duration_ms | 关键状态INFO；失败按类型定级 |
| Outbox 异常摘要 | outbox.publish.failed | outbox_id,run_id,event,provider,error_code,failed_count,oldest_age | WARN/ERROR，聚合限频 |
| 上游调用 | provider.call.finished | run_id,operation,upstream_request_id,http_status,business_code,latency_ms,retryable | 不记token/body；错误完整记录 |
| 执行重试/等待 | run.retry.scheduled / waiting_external | run_id,reason,attempt,next_retry_at,lease_epoch | WARN/INFO，避免每轮轮询重复 |
| 自动化 | schedule.fired / skipped / failed | schedule_id,occurrence_id,run_id,slot,reason,delay_ms | 已有基础，补 run_id 等串联字段 |
| 投递 | delivery.started / sent / retry / failed | delivery_id,occurrence_id,run_id,attempt,channel,error_code,upstream_request_id | 成功与失败摘要，不记录正文 |
| SSE | sse.closed / replay.failed | request_id,run_id,protocol,last_sequence,reason,duration_ms | 异常断流优先；不逐token记日志 |
| 审计持久化失败 | audit.write.failed | action,actor_id,resource_id,request_id,error_code | ERROR并计数，避免递归写同一失败审计库 |

## 五、落地顺序

### 第一批：先确保“出错能查到”
1. 统一服务/角色 logger，正确继承 request_id/run_id。
2. HTTP 完成日志、500 错误边界、安全外部错误文案。
3. 修正 panic 堆栈、请求关联和指标记录。
4. 补 Outbox/审计失败可见性，以及统一 Run/投递终态摘要。

### 第二批：再确保“没人盯着也能发现”
1. 暴露并采集所有后台角色的指标，补最后成功处理时间与积压年龄。
2. 补空定义指标及发送尝试口径，保留已有 SSE/容量/租约等指标。
3. 接入集中日志和现有监控平台，完成告警通知与查询入口。

### 第三批：提高跨进程定位效率和长期治理
1. 补真实追踪埋点及异步传播；不得把“配置 exporter”当成链路已完成。
2. 前端排查编号、必要的错误上报；敏感字段与网关日志专项检查。
3. 落实运行日志、审计、业务事件的独立留存/归档、访问控制和容量预算。

我的建议是不推翻现有 slog/指标体系，也不在所有函数里插日志。按错误边界和关键状态转换补齐，优先利用现有基础。

## 六、建议的上线验收标准

以下为本项目建议的验收目标，不是声称当前已经达到的指标：

- 任取一次受控请求失败，仅凭前端排查编号能找到 HTTP 错误及关联 Run/投递；日志没有原始凭证或正文。
- 注入受控 panic 后有完整堆栈、正确请求 ID、可检索的 500 记录及指标，进程仍按设计服务。
- 在隔离环境让 Redis 发布失败、同时让错误状态持久化失败，仍有明确异常日志/计数，不只是看到 pending 不动。
- 分别停止执行 Worker、投递 Worker、Scheduler：监控必须能区分组件失联或停止处理，不能因为 API Ready 而保持全绿。
- 执行一次成功和一次失败的 Run/投递，远端采集值按正确口径变化；不是只在本地 Registry 测试 Set/Inc。
- 告警真正送达指定责任人，恢复后有恢复通知；阈值依据期望调度周期、任务耗时和业务SLO制定。
- 容器重建后历史日志仍可检索；在批准的保留窗口内可查旧版本事件，超期按各数据类别执行清理。
- 在约定并发量下验证新增日志的量、性能、磁盘/存储预算及故障期间限频行为。

## 七、本次验证结果与限制

通过：
- TestProductionAlertingMetricsAreExported
- TestProviderCapacityMetricsAreExported
- TestSessionRevokeFailureMetricIsExported
- TestAIModelMetricsDoNotIncludeIDs
- TestAuthLogoutClearsCookiesAndReportsRevokeFailure

`internal/platform/logging` 当前没有测试文件。HTTP 测试临时目录可执行文件被 Windows 拒绝后，改为构建到本地审查目录运行，2 项定向测试通过；没有修改产品代码。

这些结果只证明既有注册/标签和指定注销失败行为，不证明日志完整、Panic 已正确处理、后台指标已被采集或告警已送达。Panic、关联传播、Outbox静默分支等结论来自源码审查，未进行真实线上故障注入。外部文档检索未返回可引用材料，本报告没有将其作为依据。
