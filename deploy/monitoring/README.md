# Worker / Scheduler 内部监控（第二批）

本目录只提供代码装配说明和示例，**不执行部署**。默认不监听，也不启动额外数据库采样。没有新增 schema、迁移、SQL 生成文件或共享 telemetry 修改。

## 开启与网络边界

各进程显式配置，建议 Prometheus/探针代理与业务进程处于同一网络命名空间：

```text
# 执行 Worker
WORKER_MONITOR_ADDR=127.0.0.1:9091
# feishu_delivery Worker：同机独立进程请使用另一个端口
WORKER_MONITOR_ADDR=127.0.0.1:9092
# Scheduler
SCHEDULER_MONITOR_ADDR=127.0.0.1:9093
```

这是三个进程的分别配置，不能在一个环境中同时设置两个 `WORKER_MONITOR_ADDR`。空值完全关闭内部 HTTP 和采样；IPv6 loopback 使用 `[::1]:9091`。解析在业务消费者启动前完成，非法配置、占用的监听端口均使启动失败。

以 `WORKER_MONITOR_` 或 `SCHEDULER_MONITOR_` 为前缀：

| 后缀 | 默认 | 校验/含义 |
| --- | --- | --- |
| `ADDR` | 空（关闭） | 显式 IP:port；禁止主机名、空 host、端口 0 |
| `ALLOW_REMOTE` | `false` | 仅接受 `true`/`false`；非 loopback 必须显式 `true` |
| `PROBE_TIMEOUT` | `1s` | DB+Redis 探针共用预算，>0 且 ≤5s |
| `SAMPLE_TIMEOUT` | `2s` | 队列+outbox+容量采样共用预算，>0 且 ≤5s |
| `REQUEST_TIMEOUT` | `3s` | HTTP 响应预算，必须大于 probe timeout，≤5s |
| `SAMPLE_INTERVAL` | `15s` | 独立下限 5s、≥2×sample timeout，且 ≤5m；从每次采样完成后开始计时 |
| `STALE_AFTER` | `2m` | ≥2×sample interval 且 ≤30m |

监控启用时，统一要求每个必需循环满足 **`STALE_AFTER > 2 × (实际循环周期 + 单轮操作预算)`**，由角色自身的只读 `MonitoringLoopBudgets` 提供，不在 cmd 复制业务常量：

- Execution：fallback 使用 `RUN_REAPER_INTERVAL` + 原 operationTimeout；maintenance 加上原有三个串行步骤的最大预算。默认周期20s、单步5s时，维护窗口必须 **>70s**。原运行周期必须为正。
- Delivery：scan 周期1s、reclaim周期20s，两者共用实际执行的恢复操作预算5s，所以窗口必须 **>50s**，默认2m合法；2s/4s配置和5s/10s配置都不会启动一个常态 NotReady 的角色。
- Scheduler：直接复用 `Run` 的规范化周期。既有调度 pass 没有统一总轮次超时，operation budget 为0明确表示“无有限执行上界”，不是虚构瞬时成功；本补丁不增加业务取消。长时间未完成的真实 pass 仍会如实 NotReady。

此外仍须满足上表的采样新鲜度窗口校验。循环变慢时应先排障，不要只加大窗口掩盖停滞。

**端点无认证、无 TLS，绝不能映射到公网、公共 Ingress、外部负载均衡器或默认公开的 Service。** 跨容器/Pod 抓取时，只有已配置私网、防火墙/NetworkPolicy（仅 Prometheus/探针来源）后才能配置 `0.0.0.0:9091` + `WORKER_MONITOR_ALLOW_REMOTE=true`。该开关只是显式风险确认，不提供任何网络隔离能力。常规 Pod HTTP 探针请求 Pod IP，不能直接访问仅 loopback 的 listener；使用同命名空间代理或经审核的私网监听方案，不要为探针直接开放公网。

## 端点语义

- `GET /metrics`：现有角色独立 telemetry registry + 新自定义 collector。仅读取内存快照，**不会在 scrape 时查库、触发调度、执行任务、重试投递或运行 reaper**。
- `GET /health/live`：内部 HTTP 进程存活；不依赖 DB/Redis，不因空队列或上游不可用引发重启风暴。开始停止时返回 503。
- `GET /health/ready`：实际 DB `PingContext`、独立探针客户端的 Redis `PING`、所有要求的真实循环最近成功、最新完整采样成功且未过期，全部满足才返回 200；否则 503。启动后尚无首轮成功时返回 503。
- 只有固定 JSON 状态；不返回原始异常、DSN、Redis 地址/密码、SQL、用户/Run/应用/目标 ID。
- HTTP header/read/write/idle 有界；metrics 同时最多 2 个请求；依赖探针同时最多 1 个。探针回调必须尊重 context；若不尊重，外层 HTTP 仍限时返回且不会再累积依赖探测协程。采样同样最多一个进行中，阻塞不会重入或触发业务。
- shutdown 关闭 listener；HTTP listener 异常退出会取消角色 context，而不是让消费者失去可观测性后静默继续。

readiness 只是观测，**不会暂停消费者或修改准入行为**；Kubernetes 把 Pod 标为 NotReady 也不会自动停止后台 Worker。

### Redis 探针隔离与生命周期

`NewRedisProbe` 仅读取已配置 standalone/cluster 客户端的内存 Options，克隆成独立客户端；不会借用 `a.Redis` 的连接池，也不修改业务 Redis 超时。复制认证/DB/拓扑/TLS设置但不输出密钥；地址 slice、TLS config 独立克隆，不继承业务 Dialer/OnConnect/连接工厂/额外缓存与pipeline池。新 Dialer 也遵守 context，包括 TLS 握手。

- `ContextTimeoutEnabled=true`；Dial/Read/Write/PoolTimeout 都为不大于 probe budget 的正值，保留更短的已有超时。
- `PoolSize=1`、`MinIdleConns=0`、`MaxActiveConns=1`，禁命令重试/cluster重定向重试；当前锁定 go-redis 版本的 DialerRetries 是总尝试次数，因此设为1而非会恢复默认重试的非正数。
- Cluster 的池大小按节点计，不冒充整个集群只会建立一条连接；HTTP gate 仍限制同时一个探针。
- 默认监控关闭时，**不创建探针客户端**；启用时构造本身也不拨号。
- 正常退出幂等关闭独立客户端。失败后立即关闭该客户端及其实际底层连接，下一次探针才新建；底层连接跟踪也处理失败握手和迟到拨号的关闭，不依赖库仅在逻辑上标为 closed。

### 采样节流与失败退避

生产最小间隔是独立的 **5s**，同时要求 **≥2×SampleTimeout**。100ms timeout / 100ms interval 不再合法，即使通过代码构造 Config 也不能绕过校验。测试使用内部等待函数或 Go 虚拟时间加速，不放宽生产规则。

每次完整采样**完成后**才等待，不使用固定 ticker，不会补跑积压 tick。连续失败后的等待为 `2×base, 4×base, 8×base...`，上限 **5分钟**；任一成功立即恢复 base 间隔。等待和采样都响应取消，失败保留旧值且不刷新 last-success。退避超过 stale window 时保持 NotReady，是故障观测，不触发任务或自动修复业务。


## 真实循环观测

回调接口均为 `ObserveLoop func(name string, err error)`，在 `Run` 之前装配，只做短暂内存更新：

| role 标签 | 必须有成功进展的 loop |
| --- | --- |
| `worker` | `maintenance`（过期租约/parked 维护及 slot cleanup）、`fallback_scan`（真实 queued 扫描） |
| `delivery_worker` | `delivery_scan`（due 查询+实际 Redis enqueue）、`delivery_reclaim`（两项回收 SQL） |
| `scheduler` | `schedule`（DB 时钟+pending admission+due scheduling） |

真实查询返回空集、cleanup 影响 0 行均是有效成功；失败不更新 last-success，最新 pass 状态变 0；没有 timer-only 心跳。Scheduler 把两次扫描及逐条操作错误合并给回调，仍保持原有遍历、顺序、事务、配额逻辑。Delivery 仅把原恢复循环抽成单次方法并增加 5s 预算及结果回报，不改 claim/send fencing。执行 Worker 保留第一批许可池、维护隔离和看门狗。

### 本批覆盖边界 / 待集成契约

1. 经明确扩展写集，`RecoverExpiredLeases`、`ExpireParkedExternalRuns` 已改为 `errors.Join` 汇总逐条失败，同时保留继续遍历、已完成事务与成功计数；任一逐条失败都会使维护 hook 报失败，不会标成 healthy。Readiness 仍只证明近期成功 pass，不等于全平台业务成功率。
2. Relay 和 DirectoryScheduler 没有被本批 hook 覆盖（写集外）。Outbox backlog 反映 relay 症状，不是 relay 的 last-success；Scheduler ready 也不是目录同步成功证明。
3. 执行消费者/reclaim 消费线程的每个 reader、每个在途业务 handler 不在 readiness 的逐线程判定范围；执行扫描/独立维护进展不等于“每个任务成功”。容量、队龄与业务错误指标必须同时看。
4. readiness 状态是本进程的内存状态，未写 DB/Redis，也没有管理 API 读取它的持久化契约。operations API 若需要跨实例健康，应通过可信内网监控系统整合，不要将本机快照伪装成全局状态。
5. 仓库已有 operations/docker/kubernetes 文档中“Worker/Scheduler 没有 HTTP 探针”的叙述需主线程合并时更新为“默认关闭，可显式开启本目录方案”。这些旧文档不在本批写集内。

## 指标与数据可信度

固定标签：`role`、`provider`；另外仅 `loop` 或 `kind` 使用有限集合。没有任何用户、任务、conversation、worker ID 或自由文本标签。执行 Worker provider 来自已注册的 provider plan，delivery 固定 `feishu_delivery`，Scheduler 为 `all`。

| 指标 | 含义 |
| --- | --- |
| `studio_role_queue_depth` | MySQL 实际 queued 数；Scheduler 是所有执行 provider，执行 Worker 是本 provider，delivery 是 pending delivery 数 |
| `studio_role_queue_oldest_wait_seconds` | 采样时队列最老项等待；使用 DB 时钟和 `MIN(queued_at)`，delivery 用 `MIN(created_at)`；含延迟重试/尚未到期项，不是“可立即执行”数量 |
| `studio_role_outbox_pending` | MySQL 全局 pending outbox 数，含尚未到期项 |
| `studio_role_provider_capacity{kind=...}` | 复用共享 capacityview 单条 MySQL 查询、同一 DB 采样时间：controlled / uncontrolled / effective；仅有 slots 的执行 provider 输出 |
| `studio_role_sample_success` | 最新完整采样成功=1；失败/尚未采样=0 |
| `studio_role_sample_last_success_timestamp_seconds` | 最后完整成功采样时间（进程时钟）；首次成功前=0 |
| `studio_role_sample_failures_total` | 采样失败次数 |
| `studio_role_loop_success{loop=...}` | 最近实际 pass 成功=1；失败/未运行=0 |
| `studio_role_loop_last_success_timestamp_seconds{loop=...}` | 最近实际成功 pass 时间；首次前=0 |
| `studio_role_loop_failures_total{loop=...}` | 失败 pass 次数 |
| `studio_role_stale_after_seconds` | 本实例配置的过期窗口，供告警与探针语义对齐 |

**必须先判断 sample success + freshness，再使用业务数值。** 失败保留上次完整成功值，不清零；首次成功前不输出新业务 gauge（健康/失败指标仍存在）。队列、outbox 与三种容量全部读取成功才替换快照。容量三项由 `capacityview.Read` 在单条 SQL 中按 run_id 合并，且 slot 过期判断统一使用数据库 `CurrentDBTime` 返回的同一时间；禁止使用进程 time.Now 或旧的三次独立 Depth 查询。队列、outbox、容量之间仍是相邻时刻的只读采样，不是整体事务快照，不得据此改准入。

已有 `studio_run_queue_depth`（仅执行 provider）、`studio_outbox_backlog`、`studio_provider_inflight`、`studio_provider_capacity_depth` 保持成功后更新兼容；错误不清零。**旧的标量 gauge 可能在首次成功前已有初始化零，旧指标不携带 freshness，不能单独作为健康依据。** 使用本目录新指标/规则更稳妥。

跨进程数值重复：全局 outbox 多实例不得求和；同 provider 的执行 Worker 读的是同一 MySQL 队列/容量，不是本机分片，也不得求和。Scheduler 总队列与各 provider 队列不要相加。建议按相同环境/provider 取 `max` 或指定单一实例，先过滤失败/陈旧样本。

### MySQL 查询成本

- runs 使用现存 `idx_runs_claim(status, provider, queued_at)` 的 active 前缀；provider 过滤参数化。
- outbox 使用 `idx_outbox_dispatch(status, available_at, created_at)` 的 pending 前缀。
- delivery 使用 `idx_deliveries_pending(status, next_attempt_at)` 的 pending 前缀；等待时间读取活跃行的 created_at。
- 显式 `FORCE INDEX` 防止优化器改走全历史表扫描；没有聚合历史成功/失败行，没有读 payload/错误文本，没有锁行/更改数据。
- 深度是精确 COUNT，开销仍为 **O(活跃积压)**，不是 O(1)。巨大积压可能超时；这种情况暴露 sample failure，而非零或截断冒充完整结果。应评估 15s 周期和 2s 总预算，避免每实例高频重扫。
- provider 容量复用主线程新增的 `internal/platform/capacityview.Read`，与 operations API 使用同一口径；本批不重写共享 helper 或 SQL 生成文件。

## Prometheus 示例

- `prometheus.example.yml`：同网络命名空间 loopback 抓取示例，实际端口与监听环境变量对应。
- `alerts.example.yml`：采样失败/陈旧、循环失败/停滞、端点缺失、队列老化、outbox 积压、失控容量示例；阈值仅为起点，必须按实际 SLO 调整。
- 抓取超时 4s 大于默认 HTTP 3s；采样预算独立，不随抓取频率加倍。
- 不在 Prometheus job 中添加同名 role/provider target 标签，以免出现 exported_role/provider 混淆。
- 发布前离线运行 `promtool check config prometheus.example.yml`、`promtool check rules alerts.example.yml`（在本目录运行）。未自动安装 promtool 或发布任何配置。

## 验证约束

测试使用 sqlmock、假回调和 loopback 测试 HTTP listener；不启动 Worker/Scheduler、不修改 `.env.local`、不访问共享 MySQL/Redis、不发飞书、不迁移、不提交推送。整体集成测试只有在隔离环境另行授权后才能补做；mock 通过不是实际索引执行计划、上游行为或生产部署证明。
