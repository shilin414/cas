# 文档索引

核对日期：2026-09-21。当前操作说明以这里列出的文件为入口。

## 开发与架构
- [当前架构与能力边界](architecture.md)
- [本地开发和验证](local-development.md)
- [环境配置与优先级](environment-profiles.md)
- [Redis 单机、Cluster 与 Redis 5](redis-topology.md)
- [部署子路径、OAuth 和代理](deployment-subpath.md)
- [企业权限与同步](enterprise.md)

## 发布与运维
1. [平台前置检查：CentOS 7.9 / Docker / Kubernetes 1.34.1](deployment/platform-prerequisites.md)
2. [人工数据库操作](deployment/database.md)
3. [Docker 部署](deployment/docker.md)
4. [Kubernetes 部署](deployment/kubernetes.md)
5. [验收、故障处理、回滚](operations.md)

配套文件在 [deploy](../deploy/)；全部是需填参数并审核的模板，不代表生产已经部署。

## 功能说明
- [管理员运行中心与并发可观测性](operations-center.md)
- [飞书分享卡片：表格、智能体和降级](feishu-share-cards.md)
- [Aily 执行过程与最终回复：边界和排查](aily-response-boundaries.md)
- [自动化编辑器](automation-editor.md)
- [业务应用](business-apps.md)
- [AI 模型管理与测试台](ai-models-implementation.md)
- [OCR 构建与发布](ocr-browser-deployment.md)
- [GLM 与内置 OCR 配置](glm-flashx-and-ocr-setup.md)

## 清理与历史

[本次清理说明](documentation-maintenance.md)记录了删除范围、替代文档及验证边界。
旧 Django 源码快照、TiDB 切换教程、阶段性整改/合并/进度报告从 docs 移除；历史仍可通过 Git 查询。
未实施的产品提案不作为部署步骤；本次移除旧提案，后续需求应基于当前实现重新评审。
