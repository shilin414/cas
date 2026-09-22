# 系统健壮性、定时任务与并发控制审计

- 审计日期：2026-09-22
- 代码基线：`aa6e4ad`；审计开始时工作区干净。
- 范围：自动化 Scheduler、执行 Worker、Provider 准入、Redis 队列、Outbox、租约恢复、飞书投递、企业同步、管理员后台。
- 方法：源码追踪、现有隔离单元测试、静态检查、原 SQL 筛选/更新谓词的内存复现。
- 边界：没有连接生产数据库、没有读取线上积压/运行容量、没有启动真实消费者或触发飞书消息、没有进行生产压测。dev/test 共用 DB/Redis，因此显式关闭真实 DB/Redis 集成测试。
- 变更：只增加本报告和本地审计记录，不修改业务代码、配置、数据库或部署。

## 一、结论

**系统有真实的自动排队和跨 Worker 并发保护，但不能据此认定高负载下已经健壮。**

已有机制不是简单的进程内计数器：MySQL 持久化 Run、事务 Outbox、Redis Streams 唤醒、Run CAS 抢占、带 epoch/token 的租约，以及按 Provider 串行化的数据库容量准入，能够处理不少重复投递和进程崩溃问题。

当前最需要优先整改的不是“再加一个并发参数”，而是：

1. 让所有执行入口和恢复入口共享相同的并发/额度规则。
2. 消除定时扫描的队首阻塞，让可执行任务确实能被扫描到。
3. 并发策略读取失败时不允许扩大额度；续租慢时不允许无限推迟停止本地执行。
4. 让投递具备与执行链相近的抢占隔离、重试时间约束和外部幂等保护。
5. 将现有内部保护变成管理员可观测、可控制、可验证的运营功能。

本文中 **P1** 表示高负载上线或扩容前应优先解决；**P2** 表示随后补齐的可靠性和运维问题，不等于无风险。以下没有把源码风险说成已经发生的生产事故。

## 二、负载较大时，究竟如何排队

### 2.1 主执行链

`定时到点/用户提交 → MySQL Run + Outbox → Redis 优先级队列 → Worker 抢占 → Provider 容量检查 → 执行 → 结果 → 独立投递队列`

| 场景 | 当前行为 | 重要限制 |
| --- | --- | --- |
| 已创建的 Run 暂无 Worker | 保留在数据库/队列等待 | 没有证明存在全平台统一 backlog 硬上限或基于机器压力的自动降载 |
| Provider 容量满 | 重新转为 queued，延后约 1 秒再尝试 | 会反复写状态、事件和 Outbox；目前还会转到 retry 优先级 |
| 同一计划上一轮未完成，overlap=queue | 自动扫描保留原 next_run_at，等待前一轮结束 | 不是为每一个错过时刻都创建独立 FIFO 条目；会遇到队首阻塞与 misfire 策略 |
| 同一计划上一轮未完成，overlap=skip | 记录跳过并推进下次触发时间 | 不排队补做这一轮 |
| run-now 遇到前一轮执行，overlap=queue | 建立 pending occurrence，之后再准入 | 默认每计划最多 1 个手动 pending，满额返回 429，而非无限排队 |
| 交互提交达到用户 outstanding 上限 | 拒绝本次提交 | 默认 20；定时链没有接入这一相同额度判断 |
| 调度延迟较久 | 按 misfire 与 deadline 配置处理 | 默认 fire_once；可能只补一次或跳过，不能承诺每个历史时点都执行 |
| Redis 唤醒丢失 | 数据库补偿扫描可重新发现 Run | 扫描线程执行长任务会拖延恢复；不是“绝不会延迟” |

默认优先级份额为交互/重试/定时 `7:1:2`，只描述正常 Redis 消费路径的消息调度，不是严格 FIFO，也不是按用户公平；容量拒绝后的串类和数据库扫描路径会改变实际表现。

**自动排队依据的是配置容量和状态，不是检测 CPU、内存、数据库压力后自动调整吞吐。**本次检索的执行/调度链及部署目录未发现这样的自适应降载实现。

证据：[backend-go/internal/automation/scheduler/scheduler.go:305](../backend-go/internal/automation/scheduler/scheduler.go#L305)、[backend-go/internal/automation/scheduler/scheduler.go:704](../backend-go/internal/automation/scheduler/scheduler.go#L704)、[backend-go/internal/automation/scheduler/scheduler.go:749](../backend-go/internal/automation/scheduler/scheduler.go#L749)、[backend-go/internal/execution/worker.go:425](../backend-go/internal/execution/worker.go#L425)、[backend-go/internal/execution/priority.go:1](../backend-go/internal/execution/priority.go#L1)。

### 2.2 配置与实际容量不能混为一谈

| 控制项 | 源码默认值/本地文件观察 | 说明 |
| --- | --- | --- |
| WORKER_CONCURRENCY | 10；本地 dev/test/prod 配置文件均为 10 | 当前不是所有执行入口共享的硬上限，见 R3 |
| Provider max_inflight | 数据库策略优先；环境默认 100 | 未查询实际数据库策略，不能将 100 宣称为线上生效值 |
| Run 租约 / 心跳 | 120 秒 / 30 秒 | 本地环境文件与源码一致；慢查询可能影响及时续租/自停 |
| 用户 outstanding | 默认 20 | 交互已接入；定时未统一接入 |
| 用户计划数量 | 默认 50 | 不等同于同时执行上限或跨用户平台队列总量限制 |
| 每计划手动 pending | 默认 1 | 只保护手动追加等待条目 |
| Scheduler 扫描 | 默认 1 秒、每批 100 | 一轮串行处理；不是保证每秒完成 100 次触发 |
| 数据库池 | 每进程默认 40 连接 | 多角色、多副本会叠加，需要做总连接预算 |
| 飞书投递消费者 | 每进程默认 4 | 当前独立于执行 Worker 的 WORKER_CONCURRENCY 参数 |

配置证据：[backend-go/internal/platform/config/config.go:302](../backend-go/internal/platform/config/config.go#L302)、[backend-go/internal/platform/config/config.go:330](../backend-go/internal/platform/config/config.go#L330)、[backend-go/internal/app/app.go:229](../backend-go/internal/app/app.go#L229)、[backend-go/internal/delivery/worker.go:51](../backend-go/internal/delivery/worker.go#L51)。

## 三、需要整改的风险

### R1 · P1：旧的等待计划占满扫描批次，后面的可执行计划可能长期饿死

**证据等级：源码确认 + SQL 筛选逻辑复现。**

- 到期查询只按 next_run_at 排序后取前 100 条。
- 对 overlap=queue 且已有 active occurrence 的计划，触发函数直接返回，next_run_at 不推进。
- 如果最旧的 100 个计划都在等待前一轮，第 101 个没有运行中任务的计划也进不了这一轮，下一轮继续遇到相同 100 个。
- 手动 pending 准入同样先取最旧 100 条，再在 Go 中检查是否可准入，因此也存在同类问题。

**复现：**构造 101 个到期计划，前 100 个有 active occurrence；连续三轮 SQL 选择都没有选中可执行的第 101 个。pending 场景也复现了 100 个被阻塞条目挤掉后面的可执行条目。没有用 SQLite 模拟 MySQL 行锁，仅复现此处相同的排序和筛选谓词。

**整改：**选择阶段过滤不可准入条目，或使用可推进的扫描游标/有界多页扫描；同一计划只选最早可准入 occurrence。最终准入仍须保持数据库行锁与复核，不应以取消锁来换速度。

**验收：**前 100 个计划阻塞时，第 101 个在有限扫描周期内准入；多 Scheduler 下每个时点仍只创建一个 occurrence。

证据：[backend-go/db/queries/automation.sql:111](../backend-go/db/queries/automation.sql#L111)、[backend-go/internal/automation/scheduler/scheduler.go:305](../backend-go/internal/automation/scheduler/scheduler.go#L305)、[backend-go/internal/automation/scheduler/scheduler.go:339](../backend-go/internal/automation/scheduler/scheduler.go#L339)、[backend-go/db/queries/automation.sql:197](../backend-go/db/queries/automation.sql#L197)、[backend-go/internal/automation/scheduler/scheduler.go:841](../backend-go/internal/automation/scheduler/scheduler.go#L841)。

### R2 · P1：读取并发策略失败会回退到默认额度，可能意外放宽全局上限

**证据等级：源码确认；生产是否触发未验证。**

启动时读取 Provider max_inflight，只有“查询成功且值大于 0”才使用数据库策略。查询错误与策略不存在/为零走同一个回退分支，使用环境默认值。容量判断随后使用各进程缓存的 MaxInflight，而不是锁内读取同一版本的策略。

例如数据库规定 20，但某个新 Worker 读取策略暂时失败并使用默认 100；它之后恢复数据库访问时，会按照自己的 100 判断准入。MySQL 串行锁只能保证计数不竞争，不能修正各 Worker 使用了不同上限。

**整改：**区分策略不存在与策略读取失败；生产读取失败应拒绝启动/暂停准入，不能扩大默认额度。策略按统一版本发布，或在准入事务内使用权威策略；调整上限时展示各 Worker 已应用版本。

**验收：**一个 Worker 无法读到策略时不能以更大的默认值启动执行；滚动扩容/额度下调后所有实例遵守相同额度语义。

证据：[backend-go/internal/app/app.go:229](../backend-go/internal/app/app.go#L229)、[backend-go/internal/execution/slots.go:148](../backend-go/internal/execution/slots.go#L148)、[backend-go/internal/execution/slots.go:280](../backend-go/internal/execution/slots.go#L280)。

### R3 · P1：本机并发限制并未覆盖恢复入口，且回收器会被长任务占住

**证据等级：调用链确认。**

常规消费者启动 Concurrency 个执行循环，但 scanLoop 和 reclaimLoop 也是独立 goroutine，并且直接同步调用执行路径，没有经过共享本机 semaphore。故配置 10 不代表本机最多执行 10 个：两个恢复入口同时工作时，理论上可能达到 12，仍受有效 Provider 全局额度约束。

更严重的是 scanLoop 先同步执行最多 10 个候选 Run，再回收过期租约、处理 parked 外部请求与清理 slot。若这些任务各执行数分钟，本应按 20 秒节奏进行的回收会拖延数十分钟。扩大消费者参数不能根治这一耦合。

**整改：**常规、数据库扫描、Redis reclaim 全部投递到统一有界执行池；数据库租约回收和外部状态协调独立循环、独立超时，不在维护循环里等待完整任务。

**验收：**三个入口同时有工作时实际 handler 活跃数不超过配置；10 个长任务也不拖延租约回收 tick。

证据：[backend-go/internal/execution/worker.go:178](../backend-go/internal/execution/worker.go#L178)、[backend-go/internal/execution/worker.go:637](../backend-go/internal/execution/worker.go#L637)、[backend-go/internal/execution/worker.go:734](../backend-go/internal/execution/worker.go#L734)、[backend-go/internal/execution/worker.go:760](../backend-go/internal/execution/worker.go#L760)。

### R4 · P1：定时任务和交互任务没有共享用户 outstanding 配额

**证据等级：入口与事务调用链确认。**

交互提交会在用户行锁下统计 outstanding，再创建 Run。定时自动触发、run-now 及 pending 转 Run 调用 createRunForOccurrenceTx，最终直接进入 CreateRunInTx；该函数保护会话串行等不变量，但不执行用户 outstanding 检查。

因此同一用户可以让多个不同计划创建的 Run 超过交互默认 20 的限制；这些 Run 还会影响后续交互请求的 outstanding 统计。Provider 总额度仍存在，但单用户隔离和前后台公平性不成立。计划数上限 50 不能替代执行额度。

**整改：**在可组合事务内实现共享准入策略，保留“交互返回拒绝、定时保留等待”这两种产品语义；统一设计用户、应用、部门、Provider 和平台 backlog 预算，明确锁顺序，避免补额度后引入死锁。

**验收：**同一用户交互和 50 个计划并发触发，总 outstanding 不突破策略；额度不足的定时触发仍有可见的等待记录，不丢时点。

证据：[backend-go/internal/execution/service.go:192](../backend-go/internal/execution/service.go#L192)、[backend-go/internal/execution/service.go:222](../backend-go/internal/execution/service.go#L222)、[backend-go/internal/automation/scheduler/scheduler.go:534](../backend-go/internal/automation/scheduler/scheduler.go#L534)、[backend-go/internal/automation/scheduler/scheduler.go:577](../backend-go/internal/automation/scheduler/scheduler.go#L577)。

### R5 · P1：投递重试的等待时间能被旧消息绕过

**证据等级：原 SQL 更新谓词复现。**

ListDueDeliveries 检查 next_attempt_at，但实际 CASClaimDelivery 只检查 status='pending'，不检查 next_attempt_at。Outbox 与每秒补扫可以产生合法的重复唤醒；发送失败后设定未来重试时间并回到 pending，队列中已有旧消息便能再次抢占并立即发送。

**复现：**next_attempt_at 设为 2099-01-01，直接执行仓库现有 CASClaimDelivery，受影响行数仍为 1。由此确认“时间限制仅在扫描处检查”不能保护真实消费入口。

**整改：**将“未到重试时间不得抢占”和重试次数限制直接写入原子 claim 谓词；对未到期的重复消息安全确认，并由持久化到期机制重新唤醒。补扫唤醒可增加去重/节流。

**验收：**连续推送 100 个旧唤醒消息，在 next_attempt_at 前不产生新的发送尝试；429 退避不被旁路。

证据：[backend-go/db/queries/automation.sql:334](../backend-go/db/queries/automation.sql#L334)、[backend-go/db/queries/automation.sql:348](../backend-go/db/queries/automation.sql#L348)、[backend-go/internal/delivery/worker.go:177](../backend-go/internal/delivery/worker.go#L177)、[backend-go/internal/delivery/worker.go:336](../backend-go/internal/delivery/worker.go#L336)。

### R6 · P1：投递缺少执行代次隔离；幂等键没有传给真实飞书调用

**证据等级：SQL 更新谓词复现 + 实际适配器调用链确认。**

投递使用 updated_at 与 60 秒期限判断卡住，没有与执行 Run 相同的 owner/epoch/token 续租保护。发送阶段还可能等待限流、用户 token、结果分享处理和多个网络调用。

旧发送者尚未结束时若被回收，新发送者可以接手；CASFinishDelivery/RequeueDelivery 只匹配 id/status，没有代次限制，旧发送者可以完成或修改新发送者的状态。复现中模拟回收→新 claim→旧完成，旧完成仍更新成功。

Worker 虽然构造了稳定的 IdempotencyKey，但 FeishuSender 调用的是 SendIMMessage；该方法转到 SendIMMessageWithUUID 时传入空字符串，实际消息请求不带 UUID。已有底层 UUID 能力，并未在这条定时投递链上接通。

**后果：**网络超时但远端已成功、租约误回收等条件下可能重复发消息；不能以数据库“只建了一条 delivery”宣称外部 exactly-once。

**整改：**claim/heartbeat/finish/requeue 使用 owner + epoch/token；设置完整发送阶段的时间预算；接通稳定上游 UUID，并核实上游幂等窗口及降级卡片策略。超出幂等保证或外部结果未知时，先核对远端状态，不做盲目自动重发。

**验收：**旧 epoch 完成必须影响 0 行；发送超过租期、远端成功后本地断连均有专门测试；验证真实 HTTP 请求包含稳定 UUID，而不仅是 fakeSender 收到了字段。

证据：[backend-go/internal/delivery/worker.go:28](../backend-go/internal/delivery/worker.go#L28)、[backend-go/internal/delivery/worker.go:214](../backend-go/internal/delivery/worker.go#L214)、[backend-go/db/queries/automation.sql:342](../backend-go/db/queries/automation.sql#L342)、[backend-go/db/queries/automation.sql:369](../backend-go/db/queries/automation.sql#L369)、[backend-go/internal/delivery/adapter.go:121](../backend-go/internal/delivery/adapter.go#L121)、[backend-go/internal/identity/feishu.go:500](../backend-go/internal/identity/feishu.go#L500)、[backend-go/internal/delivery/adapter_test.go:58](../backend-go/internal/delivery/adapter_test.go#L58)。

### R7 · P1：心跳串行且缺乏独立超时，失去租约后的本地停止可能不及时

**证据等级：源码故障路径分析；未进行真实慢库注入。**

heartbeatLoop 对所有 inflight 逐个访问数据库，调用继承进程生命周期 ctx；先等待数据库操作返回，再判断租约安全时间是否已经过去。没有每次续租的短超时，也没有独立于数据库请求的租约到期 watchdog。

DB_QUERY_TIMEOUT 默认 15 秒只是配置项；database.WithTimeout 有定义，但本次检索未发现执行/调度链调用它。连接读写超时 60 秒也不等价于连接池等待和整个事务的 15 秒截止时间。

一个慢续租可能阻塞后续所有任务。正常故障返回后确实有自停保护，但不能保证数据库卡住时也在租约到期前自停。现有 fencing 与 external submission 容量统计降低了旧写覆盖和外部重复执行风险，本条不等于这些已有保护失效。

**整改：**每次续租使用明确的短 deadline；采用有界并行或批量续租；每个执行有独立、单调时钟的安全期限 watchdog；关键心跳与一般业务查询隔离预算并监测连接池等待。

**验收：**一个续租阻塞不拖住其余任务；DB 连接池耗尽时，本地执行在安全截止时间前收到取消，而不是等数据库恢复才发现过期。

证据：[backend-go/internal/execution/worker.go:885](../backend-go/internal/execution/worker.go#L885)、[backend-go/internal/execution/service.go:733](../backend-go/internal/execution/service.go#L733)、[backend-go/internal/platform/database/database.go:35](../backend-go/internal/platform/database/database.go#L35)、[backend-go/internal/platform/config/config.go:72](../backend-go/internal/platform/config/config.go#L72)。

### R8 · P2：容量满时 1 秒重试产生写放大，并把正常任务降到 retry 队列

**证据等级：源码确认；吞吐损耗数值需压测。**

容量拒绝走 RetryOwnedRunAfter：Run 先被 claim 为 running，写 started/event，然后执行容量检查；拒绝后又改为 queued/priority=retry、写 retry event、写 Outbox、释放租约。持续饱和时形成许多“没有真正调用上游”的读写与锁竞争，多个 Relay 还可能重复发布同一未标记完成的 Outbox。

这也将本来按交互/定时份额调度的任务统一移入 retry，削弱 7:1:2 的业务含义。数据库 fallback 的有限老化奖励也不能证明低优先级永不饥饿：新定时任务 45+40 仍低于新交互任务 100。

**整改：**容量等待与业务失败重试分离，沿用保留业务优先级的 defer 语义；先获得执行许可再标记真正开始；有界抖动退避/容量释放唤醒；不应简单增加 Worker 数制造更多准入争抢。

**验收：**容量持续满载时每个等待任务的 DB 写入有上界；交互和定时不因容量拒绝而丢掉原有优先级；验证 Redis 正常和不可用两条路径的公平性。

证据：[backend-go/internal/execution/worker.go:48](../backend-go/internal/execution/worker.go#L48)、[backend-go/internal/execution/worker.go:340](../backend-go/internal/execution/worker.go#L340)、[backend-go/internal/execution/worker.go:524](../backend-go/internal/execution/worker.go#L524)、[backend-go/internal/execution/retry.go:57](../backend-go/internal/execution/retry.go#L57)、[backend-go/db/queries/execution.sql:200](../backend-go/db/queries/execution.sql#L200)、[backend-go/db/queries/execution.sql:158](../backend-go/db/queries/execution.sql#L158)、[backend-go/internal/execution/defer.go:19](../backend-go/internal/execution/defer.go#L19)。

### R9 · P2：Redis 故障时的限流退化为每进程限流，不再是全局速率上限

**证据等级：源码明确设计行为。**

共享 GCRA 失败后，使用每进程同额度的本地 GCRA。若额度是每秒 10 次，N 个进程都进入降级时，汇总速率可能接近 N×10，而非全局 10。这是有界降级而非无限放行，但对严格上游 QPS 来说仍不够。

注意：QPS 与并发数是两个控制维度；MySQL Provider slots 仍然控制运行中 Run 的有效容量，不能将本条误写成“Redis 一坏就完全没有并发限制”。

**整改：**严格渠道在共享限流不可用时暂停新发起，或采用可证明总预算的降级分配；管理端显示降级状态与告警。

证据：[backend-go/internal/execution/ratelimit.go:56](../backend-go/internal/execution/ratelimit.go#L56)、[backend-go/internal/execution/ratelimit.go:133](../backend-go/internal/execution/ratelimit.go#L133)。

### R10 · P2：指标对象存在，但 Worker/Scheduler 的关键指标未形成可采集闭环

**证据等级：服务入口和指标写入点检索。**

API 暴露 /metrics，各进程创建自己的 Prometheus Registry；Worker 与 Scheduler 入口没有启动对应指标 HTTP 服务，也未找到指标推送通道。它们在各自内存写入的容量、重试、调度指标不会自动出现在 API 进程的 /metrics 中。当前 OTLP 初始化是 trace exporter，不是这些 Prometheus 指标的跨进程汇总。

此外 QueueDepth、ScheduleQueueDelay 在仓库中只有定义/注册，没有找到实际设置或 Observe 的业务调用。指标能被注册不等于能反映积压。

**整改：**每个角色可采集指标/健康端点；数据库权威队列深度、最老等待时长、各类任务成功/失败/跳过、续租延迟、回收滞后、失控外部容量、限流降级、连接池等待均纳入告警。不要拿 Redis XLEN 直接当待办数，因为 ACK 后的历史消息仍可能留在 Stream。

证据：[backend-go/internal/platform/telemetry/telemetry.go:261](../backend-go/internal/platform/telemetry/telemetry.go#L261)、[backend-go/internal/platform/telemetry/telemetry.go:286](../backend-go/internal/platform/telemetry/telemetry.go#L286)、[backend-go/internal/platform/telemetry/telemetry.go:334](../backend-go/internal/platform/telemetry/telemetry.go#L334)、[backend-go/internal/transport/http/server.go:208](../backend-go/internal/transport/http/server.go#L208)、[backend-go/cmd/worker/main.go:139](../backend-go/cmd/worker/main.go#L139)、[backend-go/cmd/scheduler/main.go:59](../backend-go/cmd/scheduler/main.go#L59)。

### R11 · P2：已经入队的定时 Run 没有执行前重新判定到期/过期语义

**证据等级：窗口规则使用点与执行 Gate 检索。**

Scheduler 创建 Run 前检查执行窗口，但 Run 进入队列后可能等待很久；Worker 执行 Gate 检查应用、绑定、ACL、Provider 状态，没有计划 deadline 的执行前校验。手动 pending 准入检查绝对 starts_at/ends_at，但没有与自动触发相同的 execution_window_seconds/deadline_policy 延迟判断。

因此设置“超过窗口跳过”不能被理解为已排队 Run 的可靠起跑截止时间。若业务只要求“触发窗口”而允许入队后任意晚执行，需要明确标注；否则这是需要补齐的执行语义。

**整改：**区分触发有效期、队列最大等待时间、最迟起跑时间、单次运行超时；将必要截止时间持久化到 Run，执行前由统一 Gate 判定，终态与 occurrence 同步。

证据：[backend-go/internal/automation/scheduler/scheduler.go:345](../backend-go/internal/automation/scheduler/scheduler.go#L345)、[backend-go/internal/automation/scheduler/scheduler.go:833](../backend-go/internal/automation/scheduler/scheduler.go#L833)、[backend-go/internal/app/app.go:471](../backend-go/internal/app/app.go#L471)。

### R12 · P2：企业目录同步也有串行长任务与队首目标锁阻塞

**证据等级：源码确认；属于独立于业务 Scheduler 的同步链。**

DirectoryScheduler 的 tick 内同步等待完整 RunWithLease；同一实例执行长同步时不会继续扫描其他同步目标。多实例下只 PeekPendingRun 最老一条，若该目标租约被其他实例持有便直接返回，不去尝试后面的其他目标任务。

另外该同步扫描用本机时钟选 due，而业务自动化已使用数据库时钟；这是多主机时钟一致性需要统一的边界，不宜混称所有调度都由数据库时钟决定。

**整改：**扫描与执行拆开，按 target 选择可执行候选并保留 target lease；使用统一时钟；限制同步并行度，避免目录同步挤占主业务数据库。

证据：[backend-go/internal/directory/scheduler.go:47](../backend-go/internal/directory/scheduler.go#L47)、[backend-go/internal/directory/scheduler.go:73](../backend-go/internal/directory/scheduler.go#L73)、[backend-go/internal/directory/scheduler.go:99](../backend-go/internal/directory/scheduler.go#L99)、[backend-go/internal/directory/repo.go:288](../backend-go/internal/directory/repo.go#L288)。

## 四、管理员应补充哪些重要功能

现有后台已有资源管理、资源授权、用户组、权限诊断、管理员角色、组织同步、模型管理和审计日志；不建议重复建设这些基础功能。当前 Provider 页面主要展示运行时能力，不是并发运营控制台。现有单个计划启停、详情、手动触发和部分管理员单 Run 权限，也不等于具备全平台队列运维能力。

| 优先级 | 建议功能 | 最低应交付的能力 |
| --- | --- | --- |
| 第一批 | **并发与容量控制台** | 每 Provider 的策略上限、生效版本、controlled/uncontrolled/effective 容量；Worker 本机活跃数；用户/应用/部门配额；受审计的安全额度变更 |
| 第一批 | **全平台任务与队列中心** | 跨用户筛选 Run/occurrence/delivery，真实排队原因、队龄、计划时间、准入时间、真正起跑时间、执行代次、所占额度；批量操作需 RBAC 和审计 |
| 第一批 | **暂停、限流与安全排空** | 暂停新准入但允许已运行完成；单独暂停新调度/投递；Provider 或应用级隔离；受控取消；恢复可限速，避免积压一起冲出 |
| 第一批 | **角色健康与告警** | Worker/Scheduler/Relay 最后有效 tick、消费者存在性、续租失败、积压最老时间、调度延迟、失控上游执行、数据库池等待、Redis 降级 |
| 第二批 | **失败任务与异常结果处置** | 区分执行失败、投递失败、外部结果未知；可核对状态、受控重试/补发、终止/隔离；保护幂等，禁止 unknown 请求盲目重跑 |
| 第二批 | **定时任务治理** | 全平台计划清单、同一时刻触发热力图、错峰/抖动、补跑预览、misfire/skip 原因、停机后补跑预算、最大等待时间 |
| 第二批 | **容量与成本报告** | 分 Provider/应用/用户的到达率、完成率、平均/P95耗时、队龄趋势、拒绝/取消/跳过比率、调用费用与日预算；预测扩容收益而非只加并发 |

产品细节：
- 并发下调应默认“停止新增直到运行数降到新上限”，不是粗暴抢杀正在产生外部副作用的任务。
- 暂停新任务、停止已排队任务、取消运行中任务是三种不同动作，界面必须区分。
- 任务中心应展示数据库权威状态，不以某台 Worker 的内存或 Redis Stream 长度代表全局。
- 变更额度应有旧值、新值、操作者、原因、策略版本、生效情况、回滚入口；沿用已有 RBAC/审计体系。
- 当前列表接口按 caller.ID 查询计划，管理全员计划需要独立授权设计，不能仅增加一个前端筛选器。

现状证据：[frontend/src/pages/Enterprise/enterpriseNav.tsx:32](../frontend/src/pages/Enterprise/enterpriseNav.tsx#L32)、[frontend/src/pages/Enterprise/desktop/ProvidersPage.tsx:8](../frontend/src/pages/Enterprise/desktop/ProvidersPage.tsx#L8)、[backend-go/internal/transport/http/schedule_handlers.go:147](../backend-go/internal/transport/http/schedule_handlers.go#L147)。

## 五、整改顺序与验收方案

### 第一阶段：修正并发和调度正确性

R1/R2/R3/R4/R7：统一执行池与准入规则；策略读取失败不放宽；调度扫描公平推进；维护循环不执行长任务；独立租约 watchdog。

R5/R6：投递 claim 加到期条件、epoch/token fencing、完整时间预算和真实上游幂等。

这些工作应先于“扩容 Worker 或加大 max_inflight”。否则加副本可能放大准入锁竞争、重复唤醒与上游限流。

### 第二阶段：稳住饱和与故障状态

R8/R9/R11/R12：容量等待与失败重试分离、抖动退避、降级限流策略、持久化起跑截止时间、同步扫描/执行解耦。

### 第三阶段：管理闭环与压测准入

R10 与管理台第一批功能并行建设。压测必须使用隔离 MySQL/Redis 与可控制耗时的假 Provider，不应在共享 dev/test 或真实飞书渠道直接做洪峰测试。

| 场景 | 需要验证的不变量 |
| --- | --- |
| 1,000 个计划同一秒触发 | 无重复 occurrence；阻塞计划不饿死其他计划；积压与延迟可见 |
| 多 Worker + 多 Scheduler 并发 | Provider 实际运行数不突破一致生效策略；单会话与单计划重叠规则成立 |
| 正常消费/补扫/reclaim 同时执行 | 本机活跃 handler 不超过配置；维护 tick 不受任务耗时拖延 |
| 用户交互 + 50 个计划 | 统一配额成立，其他用户仍有公平进展 |
| Provider 容量持续满载 | 无 1 秒写放大风暴；不耗尽业务重试次数；优先级不串类 |
| Redis 重启/短时失联 | 无丢失持久化工作；共享限流降级符合明确策略；恢复不产生突然洪峰 |
| 数据库锁等待/池耗尽/短时失联 | 未持有效租约时不继续提交；看门狗及时停止；数据库恢复后状态可协调 |
| 投递中断/旧消息堆积/429 | 未到期不得重试；旧代次不能完成新代次；稳定 UUID 真正到达上游 |
| 暂停后恢复与额度动态下调 | 不重复执行、不粗暴释放仍占用的上游容量、恢复速度可控 |
| 队列等待超过业务时效 | 按持久化策略跳过或执行，并明确记录原因 |

压测输出必须包含：峰值实际并发、吞吐、队列等待 P50/P95/P99、最老队龄、数据库锁等待/连接池等待、心跳延迟、外部失控容量、每任务额外重试/写入次数、错误率与恢复时间。没有这些数据，不应给出“支持多少用户/多少并发”的承诺。

容量估算只是起点：在稳定负载和相同任务耗时假设下，所需执行位约为“每秒新任务数 × 平均执行秒数”；真正可用上限还受上游 QPS、数据库、投递、任务耗时分布和故障余量限制。

## 六、本次验证结果与限制

### 已运行

以下 9 个实际含测试的包通过：
- internal/execution
- internal/automation/schedule
- internal/automation/scheduler
- internal/delivery
- internal/directory
- internal/platform/config
- internal/platform/telemetry
- internal/workerdispatch
- internal/transport/http

internal/execution/invariant 没有测试文件，不计为通过测试的包。

`go vet` 对 execution、automation、delivery、directory、workerdispatch 通过。

投递与 HTTP 的临时目录测试程序曾遇到 Windows Access is denied；改为在审计目录编译测试可执行文件后运行，均 PASS。HTTP 中出现的 database unavailable 日志来自通过的错误路径测试，不是本次连接到了生产数据库。

四项 SQL 逻辑复现：
1. 到期扫描队首阻塞；
2. pending 准入队首阻塞；
3. 未来 next_attempt_at 仍能 CAS claim；
4. 回收后的旧投递完成仍能覆盖状态。

结果文件：SQL 隔离复现结果（本机文件 `.planning/robustness-concurrency-20260922/predicate-repro-results.json`，未入库）。复现使用原查询/更新谓词在内存 SQLite 执行；仅为适配 SQLite 将 CURRENT_TIMESTAMP(3) 归一化、提供等价 IF 函数。**它证明相关筛选与更新条件不足，不验证 MySQL 隔离级别、行锁、Redis 消费和端到端外部副作用。**

### 未运行 / 不作承诺

- MySQL/Redis 真实集成测试：未开启，避免共享环境副作用。
- race detector：当前 CGO_ENABLED=0，且未发现 gcc；未运行。
- 生产压测、机器资源监测、真实索引执行计划与锁等待采样：未做。
- 线上数据库 Provider 配额、部署副本数、实时队列长度：未读取。
- 上游幂等窗口：未在本次对飞书服务进行实测，不承诺外部 exactly-once。

**总评：现有实现已有较好的并发保护基础，但在“入口统一、等待公平、续租及时性、投递隔离、运行可观测”五个方面仍有高负载上线前应处理的缺口。现有测试通过与这些缺口并不矛盾，后续应优先补上述故障场景的回归测试，再进行隔离压测。**
