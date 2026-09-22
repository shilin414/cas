# Kubernetes 1.34.1 部署手册（Kustomize 5.7.1）

目标：向**已有**Kubernetes集群发布应用。Client/Server基线v1.34.1，内置Kustomize基线v5.7.1。
本手册不安装Kubernetes，不改节点OS，不在CI、InitContainer或Job执行数据库迁移。
CentOS7.9如果只是kubectl管理机，不代表节点也用同样OS；节点与运行时检查见[平台前置条件](platform-prerequisites.md)。

沿用旧环境分析里的候选参数：namespace eboat-ai-ns、Harbor harbor.hengan.com:8086/eboat2、IngressClass higress。**这些不是本次实测集群结果**，须在发布前确认。

## 0. 清单结构

- [base](../../deploy/k8s/base/)：5个Go角色+web，3个ClusterIP Service（xiaoan-api/xiaoan-stream/xiaoan-web），ConfigMap。
- [prod overlay](../../deploy/k8s/overlays/prod/)：namespace、镜像替换和Ingress。
- [RWX PVC示例](../../deploy/k8s/storage.example.yaml)：单独审核创建，不随应用清单管理生命周期。
- Secret、TLS Secret、registry Secret均由操作人预先配置，不写入Git，不经过Kustomize明文渲染。

初始所有角色1副本，Scheduler用Recreate避免正常滚动时同时运行两版；其他角色RollingUpdate。应用程序仍应使用已有数据库租约机制，不把单副本当并发控制。

## 1. 镜像与上下文

按[Docker手册第1节](docker.md)构建并推送两个不可变镜像。构建与节点架构一致；记录digest。

```bash
kubectl version
kubectl config current-context
kubectl get nodes -o wide
kubectl get ingressclass
kubectl get storageclass
kubectl get namespace eboat-ai-ns --show-labels
kubectl auth can-i create deployments -n eboat-ai-ns
kubectl auth can-i create secrets -n eboat-ai-ns
```

先确认不是错误集群。Namespace若不存在，由有权限的管理员人工创建；存在则不要重建或覆盖共享标签：

```bash
kubectl create namespace eboat-ai-ns
```

上条只用于确实不存在的namespace。禁止为了套用模板去删除已有namespace。

## 2. 配置、镜像拉取权限与证书

在受控管理机准备 /etc/xiaoan/backend.env，参考[模板](../../deploy/backend.env.example)，权限0600，填写所有真实值：正式PUBLIC_ORIGIN、生产数据库/Redis、飞书凭据、原有加密密钥、存储配置等。
格式是无引号的plain KEY=value，不用export、多行、变量引用或行尾注释。


```bash
# 不把Secret内容打印或保存成可提交的yaml。
kubectl -n eboat-ai-ns create secret generic xiaoan-secrets \
  --from-env-file=/etc/xiaoan/backend.env --dry-run=client -o yaml | \
  kubectl apply -f -
```

管道含秘密，不启用shell tracing/录屏日志，不把输出重定向到共享文件。RBAC保护Secret，base64不是加密。
所有后端角色读取同名Secret；envFrom中Secret在ConfigMap之后，冲突键由Secret覆盖，故必须核对STUDIO_ENV、APP_BASE_PATH和STORAGE_*没有旧值。

管理员应提供仅能拉取目标项目的dockerconfigjson（用专用DOCKER_CONFIG隔离，不复制个人所有registry认证）：

```bash
kubectl -n eboat-ai-ns create secret generic harbor-pull \
  --type=kubernetes.io/dockerconfigjson \
  --from-file=.dockerconfigjson=/secure-path/harbor-docker-config.json \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n eboat-ai-ns create secret tls xiaoan-tls \
  --cert=/secure-path/fullchain.pem --key=/secure-path/privkey.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

证书覆盖正式域名；Harbor CA/HTTP设置应在节点真实运行时上由平台管理员处理，Secret不解决证书不信任。

## 3. 持久化：先选一种方案

### A. 本模板默认：共享RWX目录

将storage.example.yaml复制到受控工作目录，替换StorageClass、容量和namespace，审核后手动申请：

```bash
cp deploy/k8s/storage.example.yaml /etc/xiaoan/storage.yaml
# 编辑为集群真实支持RWX的StorageClass，不能照抄REPLACE_*。
kubectl apply -f /etc/xiaoan/storage.yaml
kubectl -n eboat-ai-ns get pvc xiaoan-storage
```

必须Bound且支持多节点RWX；所有后端挂同一claim、同一路径/app/data/storage。
容器UID/GID为10001，Pod设置fsGroup10001，但NFS root-squash等情形仍需存储管理员预设目录权限，不能用chmod777绕过。
已有文件需在停写窗口迁移/核验。备份独立于Deployment。RWO不是默认替代：多个Pod/滚动副本可能分散到不同节点，出现Multi-Attach或孤立文件。

### B. S3兼容存储（需调整清单）

配置STORAGE_DRIVER=s3，S3_ENDPOINT（host:port，不含协议）、S3_BUCKET、S3_REGION、S3_ACCESS_KEY、S3_SECRET_KEY、S3_USE_SSL。
桶需预先创建并授权，程序不自动建桶。
同时从**所有后端**Deployment删除storage volumeMount和PVC volume引用，保留/tmp；Secret不要残留与新方案冲突的localfs值。API、Worker、Stream读取的是同一桶和密钥。
不做这一步即使STORAGE_DRIVER=s3，Pod也可能因仍要求PVC而Pending。

选择S3后重新render和dry-run；不要混合一部分Pod本地目录、一部分Pod对象存储。

## 4. 设置镜像、域名和入口

编辑deploy/k8s/overlays/prod/kustomization.yaml：替换两个newTag为已推送版本，或用digest固定。namespace如需变更，Secret、PVC、Ingress、命令中的namespace也同步。
编辑ingress.yaml：将REPLACE_APP_DOMAIN换成正式域名；ingressClassName使用步骤1实际存在的类。

模板默认同源路径/xiaoan-platform，不配置任何Ingress rewrite；web Nginx去除一次前缀并转发API/SSE。
正式PUBLIC_ORIGIN只填https://域名，飞书后台回调为https://域名/xiaoan-platform/auth/feishu/callback。

**Higress/LB发布门槛**：
- 保留前缀；覆盖不可信客户端的转发头；TLS在网关终止。
- SSE路由关闭缓冲/缓存，允许长连接，idle/read timeout至少覆盖预期流时长；web模板为3600s，上游每15s保活不能替代网关设置。
- 模型测试API可能运行600s，应按该路由配置响应超时；上传请求允许至少21MiB multipart。
- 具体Higress策略/注解由已安装控制器版本决定；本文不把ingress-nginx专属注解硬套给Higress。平台管理员在实际控制台/受支持路由策略中完成并导出配置，真实上传/SSE验证通过后再放流量。

后端/前端Service均为ClusterIP，只从Ingress进入；按组织NetworkPolicy限制入口和依赖出口。发布模板不含“一刀切deny all”策略，以免误阻断DNS、Cluster节点发现和内部上游。

## 5. Kustomize渲染和集群校验

从仓库根目录：

```bash
kubectl kustomize deploy/k8s/overlays/prod > /etc/xiaoan/release.rendered.yaml
# 可选独立kustomize：kustomize build deploy/k8s/overlays/prod
# 人工检查生成清单里不再有REPLACE_*，命名空间/镜像/路径正确。
grep -n 'REPLACE_' /etc/xiaoan/release.rendered.yaml
kubectl apply --dry-run=server -f /etc/xiaoan/release.rendered.yaml
kubectl diff -f /etc/xiaoan/release.rendered.yaml
```

grep有任何占位符即停止；diff退出码1表示有差异，>1表示错误。server dry-run只向当前集群校验，不创建Workload，但仍应选对context。
查看ConfigMap、imagePullSecrets、PVC名称引用是否一致。Kustomize为配置附加hash并改写引用，使配置内容变更触发新Pod。

若标准web镜像被restricted策略拒绝，停止，按平台要求改造非root镜像；不为了部署关闭安全准入。

## 6. 人工数据库准备，再发布

首次部署先完成[数据库手册](database.md)，数据库变化由DBA手动执行。已有部署升级应按第8节进入维护窗口。
确认无旧调度/投递进程在其他主机运行，避免连接同库的重复旧版本继续工作。

```bash
kubectl apply -k deploy/k8s/overlays/prod
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-api --timeout=180s
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-stream --timeout=180s
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-worker-aily --timeout=180s
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-worker-delivery --timeout=180s
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-scheduler --timeout=180s
kubectl -n eboat-ai-ns rollout status deployment/xiaoan-web --timeout=180s
kubectl -n eboat-ai-ns get pods,svc,ingress,pvc
kubectl -n eboat-ai-ns get events --sort-by=.lastTimestamp
```

等待资源使用的timeout只是操作超时，不保证应用业务健康。

## 7. 健康与业务验收

API/Stream用startup和liveness检查/health/live，readiness检查/health/ready；Worker/Scheduler没有HTTP服务，未伪造端口探针。它们的Ready仅表示容器在运行，必须进一步看日志/积压和授权测试任务。

```bash
kubectl -n eboat-ai-ns exec deploy/xiaoan-api -- wget -qO- http://127.0.0.1:8080/health/ready
kubectl -n eboat-ai-ns exec deploy/xiaoan-stream -- wget -qO- http://127.0.0.1:8081/health/ready
kubectl -n eboat-ai-ns exec deploy/xiaoan-web -- nginx -t
kubectl -n eboat-ai-ns logs deploy/xiaoan-worker-aily --tail=100
kubectl -n eboat-ai-ns logs deploy/xiaoan-worker-delivery --tail=100
kubectl -n eboat-ai-ns logs deploy/xiaoan-scheduler --tail=100
curl --fail https://REPLACE_APP_DOMAIN/xiaoan-platform/ -o /dev/null
```

DNS需指向真实Ingress入口；在临时DNS验证时用curl --resolve保持正确Host/TLS，不忽略证书错误。
继续完成[验收清单](../operations.md)：OAuth、Cookie/CSRF、真实受控Run/SSE、附件、OCR、目录/授权、明确批准的飞书测试群投递。不要向真实用户发历史任务。

## 8. 升级与配置更新

1. 保存旧release清单（不含Secret）、镜像digest、各角色副本数、schema版本，备份数据库/存储/匹配密钥。
2. 若包含数据库调整，入口维护后暂停写入并停止调度/消费；先确认无进行中的Run或已有恢复计划：

```bash
kubectl -n eboat-ai-ns scale deploy/xiaoan-scheduler deploy/xiaoan-worker-aily deploy/xiaoan-worker-delivery --replicas=0
# 需要停止所有业务写入的维护窗口：
kubectl -n eboat-ai-ns scale deploy/xiaoan-api deploy/xiaoan-stream --replicas=0
```

3. DBA人工执行新增SQL并确认dirty=0。失败不推进发布。
4. 更新镜像/配置，render、dry-run、diff后apply。清单replicas=1会恢复到1，若生产先前扩容需恢复已记录的数量。
5. 固定名字的Secret更新不会自动重建Pod；只改Secret时需要对五个后端rollout restart：

```bash
kubectl -n eboat-ai-ns rollout restart deploy/xiaoan-api deploy/xiaoan-stream deploy/xiaoan-worker-aily deploy/xiaoan-worker-delivery deploy/xiaoan-scheduler
```

6. 逐项rollout status和业务验收后解除维护。

## 9. 回滚与排错

无SQL不兼容变更时，使用**保留的旧release清单**恢复镜像及匹配配置，再逐角色验收；rollout undo只回滚Deployment模板，不回滚外部Secret、数据库或文件。
旧hash ConfigMap应保留到回滚窗口结束。禁止直接delete整个namespace、PVC或清空Redis。

- ImagePullBackOff：镜像tag、架构、pull secret、Harbor TLS/HTTP、节点出口。
- Pending：PVC Bound/RWX、挂载权限、资源配额/调度限制。
- CrashLoopBackOff：上一个容器日志、dotenv遗漏、加密密钥、DB/Redis、存储权限。
- Ready但业务失败：schema未升级、外部授权、Cluster发现节点不可达、飞书回调、网关超时。
- SSE等待到最后一次性返回：逐层排查LB/Ingress/web缓冲。

本模板没有在用户目标集群执行apply；服务器准入、CSI、Higress策略及故障恢复必须在现场验收。
