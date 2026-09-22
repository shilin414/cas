# 平台前置检查：CentOS 7.9 与 Kubernetes 1.34.1

## 1. 先区分三种职责

| 角色 | 要求 |
| --- | --- |
| 镜像构建机 | 受维护 Linux + 可用 Docker/BuildKit；容器内 Go1.27.1、Node24；能访问模块/npm/基础镜像/OCR固定资源 |
| Docker 应用主机 | 本场景CentOS7.9；复用组织已批准的Docker，不在本手册自动升级OS/内核/运行时 |
| K8s管理机 / 节点 | 管理机运行kubectl；节点负责CRI/CNI/CSI。CentOS7.9作为管理机和作为节点不是同一问题 |

用户基线：Client v1.34.1、Kustomize v5.7.1、Server v1.34.1。本文只向**已有集群部署应用**，不执行kubeadm init，不改CNI、containerd、内核或集群证书。

## 2. CentOS 7.9 的边界

CentOS Linux7 已在2024-06-30结束维护。当前Docker官方CentOS安装页要求受维护的Stream9/10，不能照搬“yum安装最新版docker-ce”到7.9并宣称受支持。

- **已有经过运维批准的Docker**：完成以下预检和目标镜像试运行，再按Docker手册部署；这不改变OS已结束维护的事实。
- **没有可用运行时或旧内核无法运行镜像**：停止发布，由基础设施团队提供受维护主机或审核过的离线运行时组合。不要关闭seccomp/SELinux绕过。

现代镜像不会替换宿主内核；Windows的静态验证不能证明7.9运行兼容。

```bash
cat /etc/centos-release
uname -r
uname -m
getenforce
systemctl is-active docker
docker version
docker info
docker compose version
```

Compose模板使用env_file的raw格式，需要**Compose2.30.0+**；不是旧docker-compose v1。插件如不能在7.9运行，先更换运维认可的管理/运行环境，不降级秘密解析方式来凑运行。
Docker网络、存储驱动、防火墙由平台统一管理。本模板仅发布443，不发布API/Stream/DB/Redis端口。
SELinux开启时共享数据挂载使用:z，独占配置/证书使用:Z；不要关闭SELinux。

## 3. Kubernetes预检

```bash
kubectl version
kubectl config current-context
kubectl cluster-info
kubectl get nodes -o wide
kubectl get nodes -o custom-columns='NAME:.metadata.name,OS:.status.nodeInfo.osImage,KERNEL:.status.nodeInfo.kernelVersion,RUNTIME:.status.nodeInfo.containerRuntimeVersion,ARCH:.status.nodeInfo.architecture'
kubectl get ingressclass
kubectl get storageclass
kubectl get namespace eboat-ai-ns --show-labels
```

确认实际版本符合给定基线。Kubernetes1.34官方cgroup v2前提包含Linux kernel5.8+、支持的运行时与systemd cgroup driver。
1.34还涉及cgroup v1场景，不应把“版本显示1.34.1”当成“CentOS7.9节点已满足推荐要求”。若节点是旧CentOS7内核，先由集群管理员确认cgroup/CRI/安全支持状态。Docker Engine本身不直接提供Kubernetes CRI，具体取决于节点运行时或适配器。

还需确认：
- Namespace是否共享，操作者是否有对应权限，不能删除重建共享namespace。
- higress是否为真实IngressClass，不能只凭旧文档假定。
- RWX CSI/共享存储、UID/GID10001读写权限；不能用每Pod独立emptyDir替代持久化。
- 节点架构、Harbor的TLS/CA/认证、Pod Security限制。
- 现有web镜像是标准Nginx root master/80端口；restricted namespace可能拒绝它，应另行制作非root镜像并调整端口，不放宽namespace安全策略。后端模板已采用非root。

## 4. 外部依赖网络

| 发起方 | 目标 | 要求 |
| --- | --- | --- |
| 构建机 | 模块代理/npm/基础镜像/OCR源 | 下载校验成功，不绕过SHA检查 |
| 后端 | MySQL 192.168.212.165:23306 | host授权、schema版本、连接数 |
| 后端 | Redis seeds 192.168.212.165:6381/6382/6383及其发布的其他节点 | 认证、DB0、全集群拓扑可达，不只Ping seed |
| 后端 | 飞书、内部业务/模型上游 | DNS、出口、TLS信任 |
| 存储读写角色 | 共享目录/S3 | 同一文件集合、读写权限与备份 |
| 浏览器 | 正式域名443、飞书登录、OCR资源 | HTTPS、完整回调、同源路径 |

如果harbor.hengan.com:8086是HTTP，必须由平台管理员确认允许的例外并配置**实际构建/节点运行时**。imagePullSecret只解决认证，不解决HTTP/TLS错配；不要盲目添加insecure registry。

## 5. 官方依据（2026-09-21核对）

- Docker安装支持范围：https://docs.docker.com/engine/install/centos/
- CentOS7维护结束：https://blog.centos.org/2024/06/june-2024-news/
- Kubernetes1.34 cgroups：https://v1-34.docs.kubernetes.io/docs/concepts/architecture/cgroups/
- Kubernetes1.34运行时：https://v1-34.docs.kubernetes.io/docs/setup/production-environment/container-runtimes/
- Compose raw env_file：https://docs.docker.com/reference/compose-file/services/#env_file
- Kustomize：https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/

本手册不自动安装、升级或重新配置以上基础设施。
