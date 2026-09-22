# 企业权限与同步

当前实现位于Go的adminrbac、enterpriseaccess、directory、accessgroup及HTTP层，前端为企业管理模块。没有Django Admin部署步骤。

## 权限

- users.is_staff为应急超级管理员标志；有效用户才能访问。
- ENTERPRISE_RBAC_ENABLED启用角色/权限码控制；关闭时沿用staff-only守卫。
- 资源ACL与管理权限不是同一层：application的all/assigned/admin_only访问范围，由ENTERPRISE_ACL_ENABLED等策略参与判断。
- 前端隐藏菜单不是安全边界；每个服务端接口检查权限。
- 系统角色由迁移初始化，不依赖seeddata工具。首个管理员按[数据库手册](deployment/database.md)核验后人工授权。
- 不能为通过验收把所有人改成is_staff或全局关闭ACL；启用新策略前先审计现有授权。

## 数据同步中心

迁移0038/0039引入目标化同步与旧调度隔离；代码注册目标directory和user_groups。API层注册sync-targets、sync-jobs、sync-batches等扩展路由。
Scheduler同时运行DirectoryScheduler。同步目标配置落在sync_target_configs，批次在sync_batches，运行历史仍在directory_sync_runs。
用户组依赖有效目录版本；失败时保留上次成功的数据，而不是清空在线目录。目录及用户组的飞书权限需由应用管理员批准。

## 上线核对

1. 数据库schema已升级；飞书应用权限和组织范围正确。
2. API、Scheduler使用同一环境、数据库、加密密钥和Redis前缀。
3. 首次同步在受控窗口执行，核对人数/组/部门差异，不直接安排高频全量同步。
4. 配置RBAC角色、应用访问范围，使用一个普通账号和一个管理员账号验收。
5. 同步与投递会产生外部效果；普通健康检查不调用真实群发或账号写操作。
