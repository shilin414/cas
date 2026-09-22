# Worker/Scheduler 监控 P2 复审整改

日期：2026-09-22。仅本任务监控代码及既有循环观测写集；未改主线程 operations/API/frontend、共享 Redis 配置或业务时限。

## 1. Redis 探针不再占用业务连接池

新增 `rolemonitor.NewRedisProbe`：

- 默认监控关闭返回 nil，不构造客户端；nil Ping 返回不可用，nil Close 安全。
- 仅从 `*redis.Client.Options()` / `*redis.ClusterClient.Options()` 内存克隆连接信息；复制凭据但不打印。
- 独立 pool，`ContextTimeoutEnabled=true`；Dial/Read/Write/PoolTimeout 均为正且 ≤ProbeTimeout；保留更短的既有值。
- PoolSize=1、MinIdle=0、MaxActive=1；cluster 按节点计。禁 command retry/redirect retry；锁定版本 DialerRetries=1 表示仅一次拨号。
- 重建 context-aware Dialer，避免克隆原先捕获业务 Options 的 Dialer 闭包；TLS也用 context。独立克隆 TLS/地址数组，不继承业务 OnConnect/连接工厂/额外缓存池。
- 入口 defer Close，失败即关闭本次客户端及其物理连接，下次探针才重建。真实黑洞测试额外发现库握手失败可能逻辑关闭但残留 socket，因此跟踪实际连接并负责关闭（包括迟到拨号）。没有修改库或业务 Redis。
- 业务 client 的 Options/连接池/生命周期均不受影响。

## 2. 健康窗口根据真实循环校验

`rolemonitor.New` 接受 `map[string]LoopBudget`，统一要求启用监控时：

`StaleAfter > 2 * (Interval + OperationBudget)`

Execution、Delivery、Scheduler 新增只读 `MonitoringLoopBudgets()`：

- Execution：引用实际 ScanEvery、operationTimeout；维护包含原三个串行操作预算。
- Delivery：引用同文件真实 scanEvery/reclaimEvery，并将两个恢复循环原本相同的5秒字面量统一为 recoveryOperationTimeout；入口不复制20秒。
- Scheduler：Run 和监控使用同一个规范化周期函数；既有 pass 没有统一总操作截止时间，零预算明确表示无有限上界。没有为了监控新增业务取消；长轮次仍可能真实 NotReady。
- 默认2分钟满足现有默认值；Delivery要求大于50秒，拒绝仅满足采样规则但小于reclaim周期的配置。

## 3. 采样限速、完成后等待和指数退避

- Config.validate / ParseEnv 均强制 SampleInterval≥5s、≥2×SampleTimeout，仍保留≤5m的上限。
- 初次采样立即进行；每轮完成后才启动下一次等待，不再使用 ticker。
- 连续失败后 delay=2×base、4×base……上限5分钟；成功后立即重置为base。
- 取消时中止等待，不发起后续查询。旧值保留和健康失败语义不变。
- 测试使用内部 wait seam/Go synctest 虚拟时间，不放宽生产配置验证。

## TDD 红绿证据

### 红阶段（本会话已实际执行）

1. `TestProductionSamplingRateCannotBecomeTightLoop`：旧代码对100ms/100ms、2s/100ms、4999ms/100ms、5s/3s均返回nil，测试失败。
2. `TestSamplingRateValidationAlsoAppliesToProgrammaticConfig`：旧代码接受程序构造的100ms间隔，失败。
3. Redis helper、统一LoopBudget、采样退避以及三个角色的周期接口测试先因未实现符号编译失败。
4. `TestRedisProbeBlackholeConnectionBudgetAndLifecycle` 初次实现中两个模式都出现“probe retained blackhole connection after failure”；新增底层连接关闭后转绿。

### 绿阶段

- rolemonitor全包：PASS，覆盖率 **98.1%**。
- execution/scheduler全包：PASS；cmd/worker、cmd/scheduler构建检查：PASS。
- `go vet`（rolemonitor/execution/scheduler/delivery/两个cmd）：PASS。
- delivery：按已授权固定 `.planning/rolemonitor-20260922/delivery.test.exe`，在其包工作目录运行 `'-test.timeout=120s'`：全包PASS，含新增真实周期校验。
- 黑洞IO复跑：standalone约80.3ms、cluster约80.9ms，预算80ms；整个黑洞测试约0.17s，关闭连接且未调用业务Dialer。测试不使用ClusterClient.PoolStats——该方法会懒加载拓扑，而非纯内存读取。
- 虚拟时间测试：每轮耗时1秒，前两次失败。采样起点严格为0s、11s、32s，证明等待10s/20s是在完成后发生。
- 抖动、失败退避cap、成功恢复、取消前不采样、取消等待不重扫均通过。

## 本轮变更范围

新增：
- backend-go/internal/platform/rolemonitor/redis_probe.go
- backend-go/internal/platform/rolemonitor/redis_probe_test.go
- backend-go/internal/platform/rolemonitor/sampling_loop.go
- backend-go/internal/platform/rolemonitor/sampling_loop_test.go
- backend-go/internal/platform/rolemonitor/review_config_test.go

更新：
- backend-go/internal/platform/rolemonitor/config.go、monitor.go、monitor_test.go、edge_test.go
- backend-go/cmd/worker/main.go、cmd/scheduler/main.go
- backend-go/internal/execution/worker.go、worker_monitor_test.go
- backend-go/internal/delivery/worker.go、worker_monitor_test.go
- backend-go/internal/automation/scheduler/scheduler.go、monitor_test.go
- deploy/monitoring/README.md、本记录，以及被忽略的本地进度文件

未启动真实业务进程、未连接共享DB/Redis、未改.env.local/业务Redis配置/生成SQL/schema、未提交推送。Relay/Directory覆盖边界不变；本轮没有更改Prometheus告警表达式。
