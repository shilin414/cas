# 部署模板

这些文件配套当前Go/React项目，使用外部MySQL和Redis。**不是一键无参数上线脚本。**

- [平台前置检查](../docs/deployment/platform-prerequisites.md)
- [Docker步骤](../docs/deployment/docker.md)
- [Kubernetes1.34.1步骤](../docs/deployment/kubernetes.md)
- [人工数据库SQL](../docs/deployment/database.md)
- [验证范围](../docs/deployment/validation.md)

目录：
- docker/Dockerfile.backend：从backend-go上下文构建；不会复制真实dotenv或数据。
- docker/compose.yaml：六服务、共享localfs、HTTPS入口，需要Compose2.30+。
- docker/nginx-tls.conf：证书由运维提供，只公开443。
- k8s/base与k8s/overlays/prod：Kustomize模板，Secret和PVC先人工准备。
- k8s/storage.example.yaml：需实际RWX StorageClass，仅管理员手动创建，不随应用自动应用。
- backend.env.example：无秘密生产变量模板；REPLACE_*必须替换，已有密钥/前缀不可无计划改变。

所有命令默认无生产SQL动作；不带migration Job、InitContainer或-migrate启动参数。
模板保留当前/xiaoan-platform/，改路径要同步前端构建、代理、后端、Ingress和探针。
正式域名、镜像版本、存储、实际IngressClass/namespace、网关策略必须逐一核对。不要把本机真实.env、Secret渲染文件、证书私钥、凭据写入Git。
