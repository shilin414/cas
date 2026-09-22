# 管理运行中心与第二批并发可观测性

更新：2026-09-22。本文描述仓库实现，不代表生产已部署。第一批并发硬化仍需随本批一致发布。

## 1. 管理员入口与能力

页面为 `/enterprise/operations`，挂载在企业控制台的平台管理中，支持桌面与手机。部署有子路径时应使用前端现有 basename，例如 `/xiaoan-platform/enterprise/operations`。

- 查看 Provider 权威额度、有效占用、受控任务、没有有效执行槽的外部执行。
- 查看排队/运行/等待输入/等待外部/取消中、pending occurrence、投递与 Outbox 积压。
- 按状态、Provider、用户 ID、应用 ID 查询任务；按创建时间和 UUID 的键集分页。
- 额度变更要求填写原因并再次确认；若旧值已经变化，409 提示刷新重试，不覆盖别人的修改。
- 所有刷新由用户显式操作，没有页面自动轮询；查询失败不把未知计数显示成零。
- 页面只显示元数据，不返回或渲染 prompt、输出、运行快照、用户令牌、密码和错误原文。
- 大整数关联 ID 从后端到页面始终是十进制字符串，不经 JavaScript Number 转换。
- 移动端直接打开/刷新页面会自行加载当前账号权限，不依赖先打开抽屉菜单。
- 页面状态告警不自动发送飞书通知，也没有取消、重试、补发、批量清队列等副作用按钮。

### 权限

| 操作 | 必需权限 |
| --- | --- |
| 查询运行快照、全平台任务元数据 | `run.monitor.read` |
| 修改 Provider 并发额度 | `provider.manage` |

复用现有 RBAC 权限；不新增或自动分配管理员权限。已有迁移中的 operations_admin 包含这两个权限，但实际账号授权需以运行环境为准。额度修改继续使用服务端 Session 与 CSRF 校验。页面显式检查已解析权限，不以浏览器里的 is_staff 字段代替授权。

## 2. 并发额度与统计语义

- 修改的是 `providers.max_inflight`。第一批修复后的执行 Worker 在准入事务内读取它；旧 Worker 未更新时不能宣称全平台已遵守新策略。
- 下调只约束新准入，不强杀已运行任务。出现有效占用高于新上限时，应等待占用收敛并核对外部残留，不能盲目重试或释放槽。
- 额度修改与审计日志在同一数据库事务内；审计失败则回滚额度。遵循与执行准入相同的 admission lock → provider row 锁顺序。
- 乐观冲突条件为 expected_max_inflight；冲突返回409。返回不明或网络异常时先刷新核对，不假设一定没有提交。
- 容量观测在一条 SQL 中按 run_id 去重，并传入同一个数据库采样时间计算槽有效性。避免读到旧快照后又用更晚的当前时间，将正常续租误报为失控。
- `running` 表示 Run 已被领取，可能仍在执行准入；真正的 Provider 容量判断使用有效占用，不用 running 行数替代。
- 快照属于有界时间内的只读观测；没有精确队列位置、ETA 或仅凭队列数量推断 Worker 死活。

## 3. API 契约（手工注册路由）

这些路由在 HTTP Server 显式注册，尚未加入主 OpenAPI 生成接口。数据类型权威来源为 `internal/operations/model.go`，前端为 `features/operations/types.ts`。

### GET `/api/v2/admin/operations/overview`

需要 `run.monitor.read`，不接受 query 参数，响应包含：

- `sampled_at`：数据库采样时间。
- `providers[]`：key/name/status/max_inflight、effective/controlled/uncontrolled_inflight、五种活跃 Run 数、最老 queued 时间与秒数。
- `totals`：五种活跃 Run 数、pending_occurrences、pending_deliveries、sending_deliveries、pending_outbox。
- `alerts[]`：code、severity（warning/critical）、可选 provider、message。
- `limitations[]`：观测范围提示。

整个读取失败返回非2xx，不发送拼凑的零快照。典型告警包括策略缺失、无有效槽的外部执行、容量下调后超额、暂停 Provider 仍有排队、最老排队至少5分钟。这些是供管理员核查的观测事实，不自动触发业务补偿。

### GET `/api/v2/admin/operations/runs`

| 参数 | 约束 |
| --- | --- |
| status | 默认 active；可选 all 或 queued/running/waiting_input/waiting_external/cancelling/cancelled/succeeded/failed/interrupted |
| provider | 可选 Provider key，最长64字符，字母开头的小写字母/数字/下划线/连字符 |
| owner_user_id / application_id | 可选十进制正整数字符串，服务端支持范围不超过9223372036854775807 |
| limit | 默认50，范围1–100 |
| cursor | 服务端返回的不透明游标，最长512字符，不可跨筛选条件复用 |

未知、重复或非法参数返回400。按 `created_at DESC,id DESC` 分页，读取 limit+1 条判定是否有下一页；不使用 offset，也不计算整库总页数。

响应为 `{results:[...],next_cursor:string|null}`。每条包括 Run UUID、Provider、运行时、状态、优先级、触发类型、字符串关联 ID、时间戳、尝试次数、错误代码、queued 秒数和可验证的 wait_reason。等待事实可为 queued、deferred、provider_paused、provider_unavailable；不提供输入输出和错误原文。

### PATCH `/api/v2/admin/operations/providers/{provider}/capacity`

需要 `provider.manage` 与 CSRF；正文最多8KiB，严格拒绝未知字段、缺字段、null和多段JSON。

```json
{
  "max_inflight": 20,
  "expected_max_inflight": 30,
  "reason": "经隔离验证后降低新任务准入额度"
}
```

新额度为1–10000的整数；旧额度允许0以便修复无效配置，最大4294967295；原因去首尾空白后1–500个Unicode字符。服务端从Session决定操作者，不接受客户端actor字段。

成功响应包含 provider、previous_max_inflight、max_inflight、effective_for=`new_admissions`、updated_at。前端还校验成功响应与提交内容匹配，不能把200空对象或代理错误HTML当成修改成功。

错误：401未登录、403未授权/CSRF、400参数、404 Provider不存在、409额度已变化、413请求过大、503不可用/请求预算耗尽。内部 SQL、权限查询错误只进入服务端日志，不原样返回页面。接口均设 `Cache-Control: no-store`。

### 查询资源预算

每个API进程最多同时4个运维读取、1个额度写入，每个操作设置5秒上下文预算（数据库服务端的实际中止行为仍需目标环境验证），饱和直接返回503，不无限排队占连接。数据库列表读取是有界分页；快照精确 COUNT 仍与活跃积压规模相关，不能把新增索引或限时误解成 O(1) 查询。

## 4. Worker / Scheduler 私有监控

配置和完整采样范围见 `deploy/monitoring/README.md`，例子见同目录 Prometheus scrape/alert 文件。

- 独立的 `/metrics`、`/health/live`、`/health/ready`，默认关闭，开启后默认要求loopback；远程监听必须显式确认并做网络隔离。
- 探针不创建Run、不发送消息、不执行补偿；业务队列处理与监控解耦。
- Ready 依据 DB/Redis 探测、被监控真实循环最近成功，以及完整采样的新鲜度；空队列成功也可Ready，部分失败不能“打点续健康”。
- 失败时保留上次成功指标，并暴露失败/陈旧状态；采集系统必须结合 freshness，不能把旧值当现在的零积压。
- 全球队列、Outbox、容量会在多个角色/副本重复观测，不能把这些 gauge 按副本简单求和。
- 本批不覆盖 Relay、DirectoryScheduler 或每个在途 handler 的完整存活状态，不将此 readiness 称为全系统或上游健康证明。
- Prometheus 配置是待审核示例，没有自动部署、报警发送或开放公网端口。

## 5. 数据库变更与发布边界

新增 `0045_operations_run_page_indexes`，只给 runs 增加创建时间和状态+创建时间分页索引，没有业务字段或数据迁移。配套up/down SQL已写入源码，但本轮没有执行。

上线前由DBA确认目标版本、表规模、空间、写入压力和 `EXPLAIN`，再安排DDL。`ALGORITHM=INPLACE, LOCK=NONE`不等于无元数据锁或零负载；不支持时应失败并重新评审，不自动退化为重型复制。

更新全部相关角色后，显式配置私有探针与抓取目标。同一主机多个Worker必须区分监听端口；不同容器/Pod应根据实际网络命名空间配置，并防止把909x监控端口直接暴露公网。

## 6. 验证与仍需验收的内容

本批测试包括 Go sqlmock/loopback、React组件与共享CSRF客户端测试，以及真实headless Edge渲染、但所有API均mock的浏览器交互。浏览器测试覆盖元数据投影、游标、超大ID、确认变更、409、错误不显示零、手机直达加载权限、只读与无权限边界；外部字体请求被本地替代，没有访问真实后端或第三方服务。

真实MySQL/Redis多进程负载、0045真实DDL up/down、生产执行计划、完整告警链、上游投递语义和race检测尚未验收。新包离线覆盖率和最终全量回归结果见本批交付记录；不得把 mock 或被跳过的测试称为生产容量证明。
