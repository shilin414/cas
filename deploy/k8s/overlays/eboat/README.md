# eboat overlay — 部署到 ai.hengan.com（Higress 网关）

这是给 `eboat-ai-ns` 集群用的 overlay，入口复用 `ai.hengan.com` 的 Higress 网关
（和 higress-mcp-admin 挂在同一域名，靠路径前缀区分）。

## 目录里每个文件干什么

| 文件 | 说明 |
| --- | --- |
| `kustomization.yaml` | 命名空间、镜像仓库、引用 base |
| `ingress.yaml` | 对外入口：`ai.hengan.com/xiaoan-platform` → `xiaoan-web:80` |
| `backend.env.template` | **你要填的**：所有环境变量 → 生成 `xiaoan-secrets` |
| `storage.yaml.template` | **你要填的**：共享 RWX PVC（填 StorageClass） |
| `deploy.sh` | 把下面所有步骤串起来的脚本 |

base（`../../base`）里已经写好、**不需要你动**：
6 个 Deployment、3 个 Service、nginx 配置、ConfigMap。

## 部署顺序

```bash
# 0. 前置检查（确认不是连错集群）
./deploy.sh check

# 1. 填两个模板
cp backend.env.template /etc/xiaoan/backend.env   # 填 DB/Redis/飞书/密钥
cp storage.yaml.template /etc/xiaoan/storage.yaml # 填 RWX StorageClass
chmod 600 /etc/xiaoan/backend.env

# 2. registry 拉取凭据（只含本项目权限的 dockerconfigjson）
./deploy.sh create_pull_secret /secure-path/harbor-docker-config.json

# 3. Secret + PVC
./deploy.sh secrets
./deploy.sh storage

# 4. 渲染并检查（渲染结果里不应再有 REPLACE_）
./deploy.sh render

# 5. 发布
export RELEASE=prod-202609221200
sed -i "s/REPLACE_RELEASE/$RELEASE/" kustomization.yaml   # 或手改 newTag
./deploy.sh render && ./deploy.sh apply

# 6. 验收
./deploy.sh verify
```

## ⚠️ 数据库必须先人工准备好

`deploy.sh` **不做任何 SQL 迁移**。首次部署前按
[docs/deployment/database.md](../../../../docs/deployment/database.md) 由 DBA 建库、
执行迁移到最新版本、初始化首个管理员。仓库最新迁移是 **0044**。

## 本集群已确认的存储选择

集群共 3 个 StorageClass，只有 `nacos3-nfs-storage` 可用：

| StorageClass | 可用性 |
| --- | --- |
| `nacos3-nfs-storage` | ✅ 唯一带 provisioner，已填入 `storage.yaml.template` |
| `test-data-agent-local` | ❌ no-provisioner + WaitForFirstConsumer，本地盘 |
| `test-data-agent-nfs-uploads` | ❌ no-provisioner，需手工建 PV，且属于别的项目 |

**NFS 权限提醒**：容器 UID/GID 固定 10001，Pod 有 `fsGroup: 10001`。
但 NFS 若开了 root-squash，fsGroup 不生效，会报 `Permission denied`。
真遇到时**不要**用 chmod 777 绕过，要找存储管理员在 NFS 服务端把目录
属主设成 10001，或用 `no_root_squash`。

## 上线前必须确认的 6 件事

1. **确认 `nacos3-nfs-storage` 真支持 RWX**（NFS 后端通常支持，但要以实际 CSI 为准）：
   ```bash
   kubectl get storageclass nacos3-nfs-storage -o yaml | grep -A5 accessModes
   kubectl get pv   # 看有没有 accessModes: [ReadWriteMany] 的 PV
   ```
   若实测只支持 RWO，改走 S3（`STORAGE_DRIVER=s3` + 从 5 个后端 Deployment
   删掉 storage 挂载，否则 Pod Pending）。
2. **`TOKEN_ENCRYPTION_KEY` 沿用原值**。如果这是接已有数据库，换 key 会解不开
   已加密的飞书 token。
3. **`REDIS_KEY_PREFIX` 沿用原值**。改了会读不到旧 Session / 队列。
4. **`DB_USER` 用运行时账号**，不要用 root。
5. **同一套库/Redis 上没有其他旧版本进程在跑**，否则调度和投递会重复工作。
6. **集群 Credential 的 Pod 网段**要包含进 `TRUSTED_PROXY_CIDRS`，否则拿不到
   真实客户端 IP，CSRF 和 Secure Cookie 判定会异常。

## 以后要扩容怎么办

初始 6 个角色各 1 副本。**单副本只是起点，不影响后续扩展**，但各角色规则不同：

| 角色 | 可扩容 | 说明 |
| --- | --- | --- |
| `xiaoan-api` | ✅ 可，但通常不必 | 无状态，nginx 轮询上游；Session 在 Redis |
| `xiaoan-stream` | ✅ 可 | SSE 长连接，Service 负载均衡 |
| `xiaoan-worker-aily` | ✅ **可以，这是 RWX 的意义所在** | 多副本并行消费，靠 Redis Stream 抢占 |
| `xiaoan-worker-delivery` | ✅ 可 | 同上 |
| `xiaoan-scheduler` | ⚠️ **永远保持 1** | 模板用 Recreate；多副本会重复触发定时任务，重复投递到飞书 |

扩容命令（配置无需改动）：

```bash
kubectl -n eboat-ai-ns scale deploy/xiaoan-worker-aily     --replicas=3
kubectl -n eboat-ai-ns scale deploy/xiaoan-worker-delivery --replicas=3
```

并发控制由业务层的数据库/Redis 租约负责（`RUN_LEASE_SECONDS=120s`、
`RUN_REAPER_INTERVAL=20s`），不依赖单副本。**但 Scheduler 绝不要加副本。**

## 和 higress-mcp-admin 的关键差异（照抄会踩的坑）

| 差异点 | 后果 |
| --- | --- |
| 这里是 **6 个 Deployment**，那边 1 个 | 只起 api 的话前端能开但 Run 永不执行 |
| 这里**必须**挂共享存储 | 不挂则用户附件写在容器临时层，Pod 重建即丢 |
| 这里 Secret **禁止**写进 yaml | 真实密钥会进 Git |
| 这里 nginx **二次转发** + SSE 独立路由 | 改路径要同步前端构建/basePath/nginx/Ingress/探针 5 处 |
| Scheduler 用 **Recreate** | 避免滚动时同时跑两版 |

## 路径说明

对外路径 `deploy/k8s/overlays/eboat/` → 文档里提到的 `deploy/k8s/overlays/prod/`
是仓库自带的通用模板（域名是 `REPLACE_APP_DOMAIN`）。两者内容等价，
本目录是适配 `ai.hengan.com + higress` 的版本。原有的 `prod/` 未被改动。
