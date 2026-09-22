# 运行验收、故障排查与回滚

## 发布门禁

- 明确版本：Git提交、两镜像digest、schema version/dirty、配置修订、备份位置、负责人。
- 所有占位符已替换；数据库调整由DBA人工执行；无自动迁移Job/InitContainer/启动参数。
- MySQL5.7、Redis全集群可达、存储持久化、飞书权限/回调、正式域名与TLS已核验。
- 六个服务齐全；不能把API/Stream Ready当成Worker或上游健康。

## 逐项验收

| 检查 | 操作与通过条件 |
| --- | --- |
| 静态页面 | HTTPS首页/深链接刷新成功，assets为真实JS/CSS，不是HTML回退 |
| OCR资源 | manifest与两个tar可下载、大小/SHA匹配；不存在资源返回404不是index.html |
| OAuth | 正式飞书账号登录并回到完整子路径；Cookie Path正确、Secure生效、CSRF写请求成功 |
| 权限 | 普通用户无越权入口/接口；指定管理员企业管理可达；已有ACL行为不变 |
| Run/SSE | 用授权的测试智能体发一条受控任务，看到连续输出、终态和持久化；刷新能恢复 |
| 附件 | 测试文件上传、读取成功；Pod/容器重建后仍可读取；不以curl成功替代浏览器上传 |
| 投递 | 仅向明确批准的测试群/用户投递，delivery从pending推进到成功；不群发历史任务 |
| Scheduler | 新建受控未来任务后观察触发；目录同步使用授权范围，不能测试性清空企业目录 |
| 模型测试 | 配置密钥后显式测试，告知供应商费用；OCR图片不上传；不执行真实账号密码重置 |

## 健康与监控

API8080/Stream8081：/health/live、/health/ready、/metrics，无单独9090监听。
Readiness只检查DB/Redis Ping，不检查SQL版本、集群全部槽、存储/飞书或消息回收。
Worker/Scheduler 新增可选私有 HTTP 探针与指标，默认关闭；启用配置和实际覆盖边界见[监控说明](../deploy/monitoring/README.md)。它不覆盖 Relay/目录同步或全部在途任务，容器 Running 或该探针 Ready 仍不等于全系统及上游健康。
管理员可在[运行中心](operations-center.md)查询容量、积压与任务元数据，额度修改需要独立权限、确认和审计。
关注Outbox backlog、执行/投递pending、最老待处理时间、Run终态、Redis错误/evictions、数据库连接、磁盘空间、SSE断连。

人工只读SQL示例：

```sql
SELECT version,dirty FROM schema_migrations;
SELECT status,COUNT(*) FROM runs GROUP BY status;
SELECT status,COUNT(*) FROM delivery_executions GROUP BY status;
SELECT target_code,enabled,last_success_at,last_error_code FROM sync_target_configs;
```

不要通过删除任务表、清空Redis、手改成功状态来“清除告警”。

## 常见问题

| 现象 | 优先检查 |
| --- | --- |
| OAuth回调拒绝/404 | PUBLIC_ORIGIN、APP_BASE_PATH、飞书白名单、重复去前缀 |
| 登录反复跳转 | HTTPS与Secure Cookie、Same Origin、代理Proto/Host、Redis Session切换 |
| worker unknown command XAUTOCLAIM | 是否运行含Redis5回退的新镜像/二进制；正常只记录一次fallback enabled |
| Redis NOGROUP | key被删/驱逐、共享环境误清库；程序重建group不代表可以忽略运维原因 |
| pending投递不动 | 投递Worker是否运行、真实飞书授权、出站网络、数据库与队列错误 |
| API Ready但集群操作失败 | 从Pod检查所有advertised节点；只Ping seed不能代表Slot路由正常 |
| SSE仅最后一次返回 | LB/Higress/Nginx缓冲、压缩聚合、读超时 |
| 执行过程混入最终答案／刷新后过程异常 | 先逐项核对原始终态 `content[]`，区分平台拼接错误与尚未证实的单块混写；见 [Aily 回复边界排查](aily-response-boundaries.md) |
| 413上传失败 | 模型multipart路径21MiB上限需每层一致 |
| 模型超时/停止后仍计费 | 网关响应超时、供应商语义；客户端取消不保证供应商取消 |
| 头像/附件重启丢失 | 挂载路径、共享PVC、S3桶、UID/GID、是否误用Pod临时目录 |
| 预设模型无密钥 | 迁移不含API Key；到连接管理单独设置 |
| 多副本模型测试状态异常 | 当前模型测试运行器有进程内状态；模板初始API单副本，扩容前做专门验收 |

## 回滚

应用镜像回滚、配置回滚、数据库恢复是三件事。只在旧代码兼容新schema时回滚镜像；破坏性DDL须DBA制定恢复/前向修复方案。
保留旧工件、hash ConfigMap、Secret的安全版本备份（不放Git）、文件/数据库快照和原加密密钥。
Kubernetes rollout undo不恢复Secret/数据库；Compose restart也不会重新读取env_file更新容器环境。
不要delete namespace/PVC、down -v、FLUSHDB/FLUSHALL或执行未经评审的down.sql。

## CI与生产的隔离

现有Actions在临时服务容器中运行测试/迁移，这是隔离验证，不是发布。不能给CI附加生产连接变量让相同测试变成生产变更。
当前push触发分支是dev/main/exam，test不触发。此次文档不增加生产CI迁移或自动发布。
