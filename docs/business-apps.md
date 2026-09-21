# 业务应用接入与上线说明

## 范围与入口
基于 `dev` 的 `e24c70a` 创建独立 `app` 分支，不自动合并 dev。
复用应用中心、应用权限、收藏、工作台路由及主题，不进入 Run / AI / OCR 执行链。

| 应用 renderer_key | 页面能力 | 上游 |
| --- | --- | --- |
| barcode-query | 20位条码；生产/流向切换；文本结果 | EBOAT queryBarcode / queryBarflow |
| material-query | 货号/69码切换；文本或JSON原始结构结果 | FDL material；未用参数固定 a |
| oa-unlock | 确认工号后解锁 | OA JOB |
| oa-password | 新密码、确认密码、二次确认 | EBOAT resetOaPwd |
| ldap-password | 目录密码修改、影响提示、二次确认 | EBOAT ldapModefyPwd |
| oa-phone | 手机号、可选办公电话/传真、确认 | EBOAT modefyPhone |
| tpm-account | 解锁、重置并解锁、锁定及风险确认 | TPM tpmUserManang |

PC：主表单/结果区 + 右侧使用说明；移动端：单列卡片、44px触控提交、数字键盘、长结果换行、底部安全区。
不提供拍照识别、智能问答、密码历史、自动重试或未经验证的“成功”提示。

## 服务端配置
在后端的 **git忽略** `.env.local` 或部署密钥系统配置以下环境变量。
用户提供的接口文档含真实凭据，不能复制进仓库；下列示例故意没有任何凭据。
本实现不修改公共业务系统账号密码进行验收，不把不可达接口作为交付阻塞。

```dotenv
BUSINESS_EBOAT_URL=https://eboat.hengan.com
BUSINESS_EBOAT_AUTH=
BUSINESS_EBOAT_USER=
BUSINESS_EBOAT_PASSWORD=
BUSINESS_EBOAT_TENANT=000000
BUSINESS_MATERIAL_URL=
BUSINESS_MATERIAL_AUTH=
BUSINESS_OA_UNLOCK_URL=
BUSINESS_OA_APP_ID=
BUSINESS_OA_SECRET=
BUSINESS_TPM_URL=
BUSINESS_TPM_USER=
BUSINESS_TPM_PASSWORD=
# JSON对象：平台 users.id -> 已人工/企业目录核验的工号。
BUSINESS_ACCOUNT_MAP={}
# 获准代办的本平台 users.id，逗号分隔；留空则无人可代办。
BUSINESS_OPERATOR_IDS=
```

- EBOAT URL 只填源站，其余URL填文档完整接口地址。Basic / AppCode头需保留前缀。
- 配置缺失时，页面明确显示“尚未配置”；**没有将缺失配置伪装成空查询结果**。
- 非代办人员只能操作映射到本人的工号；客户端传其他工号会被后端拒绝。不会信任展示ID、任意输入或通用管理员标志。
- 代办人员仍需通过应用ACL。新增应用迁移默认 `admin_only`，管理员按实际需要通过现有企业管理分配访问范围。
- 配置/身份映射变更需要重启API。此阶段以人工核验映射为准，未来可替换为受控企业目录工号字段。
- OA解锁与物料原始接口使用HTTP；生产建议以HTTPS网关/VPN接入。LDAP上游将密码置于GET查询参数：生产代理和网关必须关闭或脱敏该路径查询日志；本应用对浏览器只暴露POST JSON，且不会记录上游URL。

## 本地接口契约
需要平台登录Session；POST沿用全局CSRF校验，所有响应 `Cache-Control: no-store`。
- `GET /api/v2/applications/{id}/business/status` -> `{ configured, account, can_manage_others, can_submit, reason? }`
- `POST /api/v2/applications/{id}/business/execute` -> `{ data, message }`
- 输入为严格JSON，最大8KB，不允许未知字段或多段JSON。仅使用应用记录中的已知renderer，不接受客户端指定URL。
- 条码：`{ action: "production" | "flow", barcode: "20位数字" }`
- 物料：`{ action: "article" | "ean", query: "货号或69码" }`
- 写操作：`{ usercode, confirmed: true, ...业务字段 }`；`usercode`留空可用已核验映射补全。
- OA/LDAP密码：`password`；OA手机号：`mobile, officephone?, fax?`；TPM：`action: unlock | lock | reset, password?`。
- 密码本地基线12—64位、包含字母和数字、无空白或控制字符；上游更严格规则仍可能拒绝。TPM reset固定传 `lock=0`。
- 错误响应 `{ detail }`：400输入、401未登录、403工号权限、404应用不可见/停用、429限流、503未配置/依赖不可用、502上游未确认成功。

## 安全与故障语义
- 每次状态/执行均重新检查应用启用与ACL；隐藏UI不是权限边界。
- Redis GCRA按用户、敏感目标账号各5次/分钟；Redis故障采用既有进程内有界回退（多副本总限额可能放大，需监控）。
- EBOAT token按过期时间缓存并串行刷新；401清缓存供下次手动请求重新认证，**不重放此次写请求**。
- 连接15秒、整个调用20秒、浏览器25秒；不跟随重定向，响应最大2MiB。
- 上游错误不包含URL、凭据、原始响应或密码。写操作仅 `code=200` 且无显式失败才判成功；未文档化的TPM成功格式要先补契约测试再适配。
- 账号操作执行前审计必须落库，失败则不调用上游；执行后记录 succeeded / unconfirmed。取消浏览器请求后仍尝试记录结果；不记录密码、手机号、token、原始响应。
- 超时/网络错误可能已在上游生效，页面提示先核实，不自动重试。

## 迁移与回滚
- 执行现有迁移程序应用0042，保留原有四应用及ACL不变。
- 新增LDAP/手机号/TPM三条应用，无新表。down仅停用新增应用，避免删除收藏、授权或审计关联。
- 回滚后再次up因 INSERT IGNORE 不会自动重新启用，需管理员在应用管理主动启用。
- 不在共享开发库上运行破坏性down或更改真实员工信息。上线前在测试库验证迁移，在真实账号写操作上由业务负责人执行受控验收。

## 验证记录
见同分支 `.planning/2026-09-21-business-apps/progress.md`；区分mock协议验证和真实上游连通性，不把模拟结果当作真实业务成功。

### 上线授权前置条件与待确认协议
使用人员/部门/群组授权（assigned）时，必须开启 `ENTERPRISE_ACL_ENABLED=true`；`ENTERPRISE_RBAC_ENABLED` 只控制管理权限，不能替代消费侧ACL开关。启用前应完成现有通讯录同步和平台用户关联，再由管理员分配应用权限。若仍使用旧公开/私有模式，assigned授权不会被消费侧读取，不能把保存成功当作权限生效。验收必须包括被授权和未授权的普通用户。

物料接口只读实测返回 `output/code/message` 顶层结构；连接器保留完整结构，也支持纯文本或未知JSON对象，不将业务字段 `code` / `data` 无条件当作EBOAT信封。

TPM原文没有响应格式；OA解锁示例键未加引号，可能仅为文档简写。当前保守接受严格JSON、`code=200`且无顶层/嵌套显式失败。两者的实际写操作成功/失败响应仍需接口方提供脱敏样本并由业务方批准测试账号验收，未据mock宣称真实写入已验收。

## 迁移验证命令（不会给共享应用表造数）
`BUSINESS_APPS_MIGRATION_VERIFY=1 go test ./tests/integration -run '^TestBusinessAppsMigrationTemporaryTable$' -count=1 -v`
只在单一连接的临时表副本中验证0042；不必打开 `STUDIO_TEST_DB` 或启动/停止任何worker。

## 查询结果转发到飞书

### 用户操作
条码（生产/流向）和物料（货号/69码）查询成功后，结果区出现“转发到飞书”。用户/群组选择、搜索分页、最近目标、最多20个目标、部分失败保留及重新授权入口复用对话转发组件。

卡片展示应用名称、查询条件、原查询时间（北京时间）和可直接阅读的结构化查询结果；多记录按“记录 + 字段：值”排版，并提高查询卡片的信息容量，正常结果无需收件人再打开网页。超长字段或超大结果仍会在安全上限内标记节选，避免发送超大卡片。按钮文案为“打开查询应用”，进入同一应用及不可变快照；打开页面需要平台登录和当前应用ACL，不新增匿名公开分享页。仅原查询者能使用平台再次转发。

编辑查询条件、切换查询方式、重新查询会清除旧结果的转发入口。飞书授权恢复及未登录收件人完成登录后，保留同一个快照token，不重新调用业务接口。普通查询页发生平台401时，也会在清除登录状态之前捕获当前快照的安全返回路径；只保留恢复标识，不保存业务正文，结果清除或路由离开后注销。飞书卡片本身仍可在飞书侧转发；24小时到期限制的是平台完整结果链接，不会撤回已经送达的卡片摘要。

### 服务端与故障边界
- 成功查询由后端生成随机256-bit token，将条件/时间/结果快照保存到现有Redis，24小时自动过期。返回值新增可选 `snapshot`（token、query_label、query_value、queried_at、expires_at、can_forward）。
- Redis不可用时查询本身仍成功，页面提示本次结果暂不可转发；不回退为相信客户端提交正文。
- `GET /api/v2/applications/{id}/business/snapshots/{token}`：登录、应用启用及ACL检查；只读原结果，不查询上游、不暴露owner内部ID。
- `POST /api/v2/applications/{id}/business/forward`：`{ snapshot_token, targets: [{target_type: "user"|"chat", id}] }`。严格JSON8KB、1—20个去重目标、owner/app匹配、CSRF及每用户10次/分钟限流；绝不接受客户端结果正文或外部转发URL。
- 仅使用调用人的飞书user access token，不切换为机器人身份。卡片链接源于服务端 `PUBLIC_BASE_URL`（含配置解析后的部署子路径），不信任请求Origin。
- 最大4个并发发送、单目标8秒、总45秒；按目标返回成功/失败。UI仅保留未成功目标。
- 使用固定provider UUID并记录本地发送状态。飞书UUID仅提供有限去重窗口，不能单靠它保证24小时不重复：快照及目标状态放在**同一个Redis hash**，共用过期/淘汰边界。
- 发送前写processing状态，90秒内视为忙；进程中断或超时后状态仍保留到快照过期，并作为uncertain阻止再次投递。明确非零业务错误（含HTTP400里的授权错误）判定为未接受，可修正后重试；网络断线、无法确认的HTTP/响应错误则不释放发送权，提示去飞书核实。
- 正文、token、用户/群目标清单不写业务日志；只记录操作者、应用与成功/失败数量。查询结果仅在限时Redis快照中保留，不建立Run、对话或AI任务。

### 部署与验收
无需新增数据库迁移或OAuth scope；复用现有IM与通讯录用户权限。需要Redis可用、PUBLIC_BASE_URL可被收件人浏览器访问、前后端同版本发布，并按需分配应用ACL。

Redis验证可单独执行：`BUSINESS_APPS_REDIS_VERIFY=1 go test ./internal/businessapps -run '^TestQuerySnapshotRedisLifecycle$' -count=1 -v`。仅使用随机独立命名空间和准确已知key清理，不触发共享任务/投递队列。

本轮通过组件、后端mock和独立Redis键验证，不发送真实消息。真实飞书端到端及浏览器验证仍需受控验收；本轮浏览器脚本被执行策略拒绝，未声称完成浏览器验收。
