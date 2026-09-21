# 企业数据同步中心能力设计

日期：2026-09-20
状态：已确认，待按本设计重构现有 Directory Sync

## CAPABILITY

企业管理员可以把不同类型的飞书企业数据作为独立的“同步目标（Sync Target）”配置、运行、观察和重试；“同步全部”只负责创建一个包含多个目标任务的批次，不再把部门、用户、用户组以及未来数据源硬编码成一个不可拆分的大任务。

首批同步目标：

- `directory`：部门、用户、用户所属部门关系。
- `user_groups`：普通用户组、动态用户组、用户组成员。

## CONSTRAINTS

### 固定规则

1. 一个同步目标内部必须原子发布：不能发布半份目录或半份用户组快照。
2. 不同同步目标独立成功、失败、重试；`user_groups` 失败不能回滚或标记 `directory` 失败。
3. 已发布数据采用 last-known-good：新快照失败时保留上一个成功版本，禁止清空线上数据。
4. `user_groups` 依赖一个已经成功发布的 `directory` 版本，但单独同步用户组时不强制重新拉取目录。
5. “同步全部”按照依赖 DAG 排序；当前顺序为 `directory -> user_groups`。
6. 每个目标拥有独立自动同步配置、最近成功时间、下次执行时间和错误状态。
7. 新目标不能要求修改同步运行表来增加专用计数字段；目标指标存入通用 JSON metrics。
8. 现有 `directory.sync.read/manage` RBAC 权限继续控制整个同步中心，暂不为每个目标拆权限。

### 信任边界

- 飞书 tenant token 只在服务端使用。
- 目标适配器只能写自己拥有的 live/stage 数据表。
- 调度器通过 DB lease 保证同一部署只由一个执行者领取任务。
- 批次状态由子任务归约，不能由前端推断或修改。

## IMPLEMENTATION CONTRACT

### 核心对象

#### SyncTargetDefinition（代码注册表）

- `code`
- `display_name`
- `description`
- `dependencies[]`
- `runner`
- `metrics_schema`

目标定义属于代码，不由数据库任意创建，防止数据库中出现没有执行器的目标。

#### SyncTargetConfig（数据库配置）

- `target_code`
- `enabled`
- `schedule_type`
- `interval_minutes`
- `daily_time`
- `timezone`
- `next_run_at`
- `last_run_at`
- `last_success_at`
- `updated_by`

#### SyncBatch

- 表示一次“同步全部”或多目标同步请求。
- 只聚合任务，不直接拉取或发布数据。
- 状态：`pending / running / partial_success / success / failed`。

#### SyncJob

- 一个任务只对应一个 `target_code`。
- 状态：`pending / blocked / running / success / failed`。
- 保存 `metrics_json`、`warnings_json`、错误码和错误信息。
- 可通过 `batch_id` 归属于批次，也可以是独立手动任务。

### 执行接口

```go
type TargetRunner interface {
    Code() string
    Dependencies() []string
    Run(ctx context.Context, job Job) (Result, error)
}

type Result struct {
    Metrics  map[string]int64
    Warnings []Warning
    Version  int64
}
```

调度器只认识 TargetRunner 接口，不认识部门、员工或用户组的具体逻辑。

### 首批适配器

#### DirectoryTarget

1. 拉取部门和员工。
2. 校验字段权限、父子关系、异常行和快照缩水。
3. 写 directory stage。
4. 原子发布部门、用户、用户部门关系和 closure。
5. 增加 directory version。

#### UserGroupsTarget

1. 确认存在成功的 directory version。
2. 拉取普通组、动态组及其成员。
3. 根据当前 live directory 映射 open_id。
4. 未映射成员记入 warning；超过安全阈值才拒绝发布。
5. 写 user-group stage。
6. 原子发布飞书权限组及成员。
7. 不修改部门和用户数据。

### API

```text
GET  /api/v2/admin/sync-targets
GET  /api/v2/admin/sync-targets/{target}/config
PUT  /api/v2/admin/sync-targets/{target}/config
POST /api/v2/admin/sync-targets/{target}/jobs
POST /api/v2/admin/sync-batches          # { targets: [...] }
GET  /api/v2/admin/sync-jobs
GET  /api/v2/admin/sync-batches/{id}
```

兼容期内保留旧 `/directory/sync*` API，并转发到新模型；前端迁移完成后再废弃。

### 页面

- 每个目标一张配置卡片，可独立开启定时同步和立即同步。
- 顶部保留“同步全部”。
- 历史记录按 Job 展示目标、指标、告警和批次。
- 批次详情展示依赖顺序及每个 Job 的独立结果。

## NON-GOALS

- 本阶段不做任意第三方数据源的低代码配置。
- 本阶段不允许管理员从 UI 创建代码中不存在的 Target。
- 本阶段不拆分到每个 Target 的 RBAC 权限。
- 不保证不同 Target 在同一数据库事务中提交；一致性由依赖版本和 last-known-good 保证。

## ROLLOUT

1. 新建通用 target config、batch、job 数据模型。
2. 将现有历史运行记录迁为 `legacy_full`，只读保留。
3. 抽出 `DirectoryTarget`，保证目录同步行为不变。
4. 抽出 `UserGroupsTarget`，验证失败隔离。
5. 新 API 与旧 API 双写/转发。
6. 前端切到目标卡片与独立历史。
7. 稳定后移除旧的组合执行路径。

## ACCEPTANCE CRITERIA

- 单独同步目录时不调用飞书用户组 API。
- 单独同步用户组时不调用部门/员工 API。
- 用户组同步失败后，目录任务仍可成功且现有用户组数据不被清空。
- 自动同步可为两个目标设置不同周期。
- “同步全部”生成两个可独立观察的 Job，并按依赖顺序执行。
- 新增第三个测试 Target 时，无需修改 Job 表字段、调度器主循环或前端历史表结构。
