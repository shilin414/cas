# 第二批 Worker/Scheduler 可采集监控交付记录

日期：2026-09-22。仅完成代码与离线验证，没有部署，也不是生产健康证明。

## 已完成

- 角色独立内部 HTTP：`/metrics`、`/health/live`、`/health/ready`；默认关闭，显式 role-prefixed 环境配置，默认拒绝非 loopback。
- 短超时、探针并发限额、只读后台采样；任何探针和 scrape 均不触发业务。
- readiness = DB/Redis 实际探测 + 所需真实循环近期成功 + 完整成功采样未过期。空队列算成功，失败/停滞不靠 timer 续健康。
- 活跃状态索引查询 queued 数/最老等待、pending outbox；初始不伪造业务零，失败保留最后成功快照并暴露失败/陈旧。
- 容量复用主线程新增 `capacityview.Read`，DB `CurrentDBTime` 明确传参，一次 SQL 返回 effective/controlled/uncontrolled；不再调用三次独立 Depth。
- 不改第一批并发/许可/租约/claim 修复，仅增加 hook 和错误观测通路。
- 经主线程协调扩展范围，两种执行维护方法汇总逐条错误，保留遍历、已经成功提交的事务和 count。测试证明部分成功 count=2、两个错误均保留，Worker 不会报健康。
- Scheduler 扫描和逐项错误传给 hook；原 deadline-skip/已删除 pending 的吞错支路改为返回观察到的错误，保持后续 SQL/advance/commit 的原顺序。
- Prometheus 抓取、带 sample freshness 门槛的告警示例和敏感信息/网络隔离说明。

## 本任务修改文件

### 已有文件（保留第一批未提交变更）

1. `backend-go/cmd/worker/main.go`
2. `backend-go/cmd/scheduler/main.go`
3. `backend-go/internal/execution/worker.go`
4. `backend-go/internal/automation/scheduler/scheduler.go`
5. `backend-go/internal/delivery/worker.go`
6. `backend-go/internal/execution/recovery.go`：仅 `RecoverExpiredLeases` 的错误汇总。
7. `backend-go/internal/execution/submission.go`：仅 `ExpireParkedExternalRuns` 的错误汇总。

### 新代码与测试

- `backend-go/internal/platform/rolemonitor/config.go`
- `backend-go/internal/platform/rolemonitor/monitor.go`
- `backend-go/internal/platform/rolemonitor/sampler.go`
- `backend-go/internal/platform/rolemonitor/monitor_test.go`
- `backend-go/internal/platform/rolemonitor/edge_test.go`
- `backend-go/internal/execution/worker_monitor_test.go`
- `backend-go/internal/execution/maintenance_errors_test.go`
- `backend-go/internal/automation/scheduler/monitor_test.go`
- `backend-go/internal/delivery/worker_monitor_test.go`

### 新监控示例/文档

- `deploy/monitoring/README.md`
- `deploy/monitoring/prometheus.example.yml`
- `deploy/monitoring/alerts.example.yml`
- `deploy/monitoring/IMPLEMENTATION.md`（本记录）

`deploy/monitoring` 下的 task_plan/findings/progress 和 coverage.out 为被仓库忽略的本地过程记录；固定路径测试产物位于 `.planning/rolemonitor-20260922/`，无需提交。

`internal/platform/capacityview/**` 由主线程新增，本任务只读取并复用，没有修改它。主线程 operations/API/frontend 等并行变更没有回退或覆盖。

## 离线测试结果

环境：`GOPROXY=off`、`GOSUMDB=off`，实际外部集成开关 `STUDIO_TEST_DB=0`、`STUDIO_TEST_REDIS=0`。

- TDD：先新增测试，观察到未定义 API/hook 的失败后实现；逐条维护失败测试先证明旧实现返回 `count=2, err=nil`，修复后通过；Scheduler 吞错与新容量采样契约也先红后绿。
- `go test ./internal/platform/rolemonitor -count=1 -coverprofile=...`：PASS，**98.6% statements**。
- `go test ./internal/platform/rolemonitor ./internal/execution ./internal/automation/scheduler ./internal/platform/capacityview ./cmd/worker ./cmd/scheduler -count=1`：PASS；cmd 包无单测，但完成构建检查。
- `go vet ./internal/platform/rolemonitor ./internal/execution ./internal/automation/scheduler ./internal/delivery ./cmd/worker ./cmd/scheduler`：PASS。
- 常规 `go test ./internal/delivery` 被 Windows 拒绝启动临时 test.exe。按主线程确定的验证方案，`go test -c -o ../.planning/rolemonitor-20260922/delivery.test.exe ./internal/delivery` 后，在 `internal/delivery` 工作目录运行固定绝对路径、传 `'-test.timeout=120s'`：**全包 PASS**。未修改安全策略。
- `git diff --check`（本任务已有文件）：PASS；仅提示仓库既有 LF/CRLF 转换警告。
- 两份 YAML 的本地语法解析：PASS。**未安装/运行 promtool，不能声称 PromQL 规则引擎验证通过。**

覆盖：默认关闭/私网显式确认/配置非法值，startup 与 shutdown，端口冲突，DB/Redis失败，probe/采样超时及并发上限，采样成功不能掩盖循环停滞，循环成功不能掩盖采样停滞，失败留旧值，初始化不伪造零，重复注册不 panic，SQL query/clock/capacity failure，DB 时间参数传递，空队列，逐条部分成功，delivery enqueue 失败，scheduler 吞错支路。

## 待集成 / 不承诺

- 共享 capacityview helper 必须与本次代码一起集成。
- Relay 与 DirectoryScheduler 没有 hook；不能将本次 readiness 称为全系统健康。没有逐个执行 reader/在途业务 handler 的进展断言。
- MySQL active-index 实际执行计划、规模下查询预算、网络隔离、Prometheus/告警规则加载需在隔离验收环境进一步验证；精确 COUNT 仍是 O(活跃积压)。
- 发布前运行 promtool；在主线程拥有的部署文档里同步更新“Worker/Scheduler 没有 HTTP 探针”的旧说法。
- 未启动任何真实业务进程、未连接共享 DB/Redis、未改 `.env.local`、未迁移、未增 schema、未编辑生成 SQL、未编辑 shared telemetry/config、未提交或推送。
