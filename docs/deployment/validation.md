# 文档与部署模板验证记录

验证日期：2026-09-21。环境是Windows开发机；没有连接生产DB/Redis，没有使用kubeconfig访问目标集群，也没有创建Docker容器。

## 已完成

| 项目 | 结果与边界 |
| --- | --- |
| Kustomize | 实际运行v5.7.1 build；渲染12个资源：6 Deployment、3 Service、2 ConfigMap、1 Ingress |
| Kubernetes API结构 | 12资源对照官方v1.34.1 swagger definitions离线验证；正确处理Kubernetes IntOrString标量。不替代服务器准入/策略检查 |
| Compose | 使用官方v2.30.3程序（下载SHA256核验）执行config --quiet及渲染；使用无秘密的示例变量，不连接Docker daemon |
| 服务拓扑 | 六角色完整；无migration服务/Job/InitContainer，无启动-migrate；API/Stream不发布宿主端口 |
| Nginx | 用本机1.27.5对两份配置运行-t；仅在临时副本把服务DNS换为loopback，并为TLS生成临时证书；未启动服务器，临时证书已清理 |
| Linux构建 | Go1.27.1、GOOS=linux、GOARCH=amd64、CGO_ENABLED=0，api/stream/worker/scheduler/migrate五个入口全部编译通过 |
| SQL文档 | 实际执行手册中的离线生成器，0→44输出44项、43→44输出1项；只生成受控临时SQL文件，未执行SQL |
| 前端部署测试 | test:deployment 22项通过，test:ocr-deployment 5项通过 |
| 命令语法 | 23段Bash代码通过bash -n，仅解析不执行 |
| 文档/秘密 | 检查当前Markdown内部链接、代码围栏、控制字符、真实本地env秘密值泄漏；通过；git diff --check通过 |

验证工件保存在本机被Git忽略的.run-logs/docs-verify目录，不作为部署必需文件。
本次同时将backend-go/.env.example的行尾注释移至独立行，避免当前简易dotenv解析器把注释误作地址或存储驱动值。

## 仍须目标环境验证

1. CentOS7.9实际Docker/Compose/内核/SELinux是否能运行镜像；官方维护状态不能靠本地编译消除。
2. 两个镜像真实Docker build/run、供应链扫描及目标节点架构；本文没有Docker daemon，不声称镜像构建测试通过。
3. Kubernetes server dry-run、Pod Security/配额、Harbor认证/CA、IngressClass、RWX CSI/PVC与文件读写。
4. 正式域名、TLS、飞书回调、Higress/LB长连接与上传/响应超时。
5. DBA人工SQL变更、备份恢复、首个管理员核验。
6. 生产Redis Cluster实际Slot路由、故障恢复与受控业务回收；不是只Ping入口。
7. 授权的Run/SSE、上传/下载、模型/OCR、目录同步及测试群投递验收。

没有执行生产发布、数据库变更、服务重启、CI修改、Git提交或远程推送。
