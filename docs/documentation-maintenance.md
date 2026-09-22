# 2026-09-21 文档清理记录

## 核对范围

根README、后端README、ENTERPRISE入口与docs。依据当前Go入口、配置加载器、路由注册、前端锁文件/构建脚本、0044及以前迁移、现有CI。没有读取或复制真实配置密码到文档。

## 删除范围

本次移除 **384个Git跟踪的过时文件**：
- docs/archive下339个历史文件，包括退役Django源码快照、其管理命令/配置/测试和旧交接记录。
- docs根目录45份旧进度、TiDB迁移、阶段性整改/审查、分支合并报告及未实施/已被实现取代的设计提案。

它们描述不同时间点和不同分支，已不能作为当前操作依据。未跟踪的本机材料未擅自删除；历史可在Git中查看本次变更前版本，不再作为现行文档入口。

## 替代关系

| 旧内容 | 当前入口 |
| --- | --- |
| Django、TiDB、三进程启动、G0—G15进度叙述 | [架构](architecture.md)、[本地开发](local-development.md) |
| 分散环境变量/通用.env.local教程 | [环境配置](environment-profiles.md) |
| 旧生产分析/容器暂未完成的计划 | [平台检查](deployment/platform-prerequisites.md)、[Docker](deployment/docker.md)、[Kubernetes](deployment/kubernetes.md) |
| 自动/混合SQL迁移步骤 | [人工数据库手册](deployment/database.md) |
| 旧分支截图/测试数字作为现状 | 当前功能文档与[逐次验收](operations.md) |

## 明确修正的事实

1. 六个常驻角色，投递Worker和Scheduler不可遗漏。
2. Metrics与健康检查在API/Stream端口；没有由METRICS_ADDR启动的9090监听。
3. ADMIN_BOOTSTRAP配置目前不创建管理员；需现有身份或人工核验授权。
4. 通用.env.local已停用，但加载器还保留旧回退；test还会回退开发文件，不能据此宣称隔离。
5. 前端.env.development在本机存在但未跟踪；Vite代理覆盖读process.env，不承诺放dotenv就生效。
6. 飞书最终回调由PUBLIC_ORIGIN与APP_BASE_PATH拼接，不能仅改旧回调变量的path。
7. OpenAPI之外仍有手工路由注册；模型测试台不是正式Run Provider集成。
8. Redis5回退可用于单机/Cluster，但真实集群业务与故障验证仍需单独验收。
9. CentOS7.9结束维护；现有Kubernetes1.34.1集群应用部署不等于在7.9上安装新集群。

## 配套交付及未执行范围

新增deploy下的后端Dockerfile、Compose、TLS Nginx、Kustomize base/prod overlay、PVC与无秘密变量模板。
SQL只在手册中提供人工语句/离线SQL生成方法，未执行生产SQL。没有迁移服务、Job、InitContainer或CI自动数据库变更。
没有连接/修改生产数据库、重启现有应用、推送远程或向目标集群执行apply。

验证明细见[文档验证记录](deployment/validation.md)。上线仍需目标环境完成镜像构建运行、Ingress/CSI/Secret准入和业务验收。
