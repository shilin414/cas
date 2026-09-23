# xiaoan-platform 部署手册(K8s + Jenkins + Harbor + Higress)

对应参考文档《小安工作助手 CAS 部署改造执行指南》的落地实现。本文档是**执行步骤**,按顺序走完即完成首次部署;之后日常发布只需在 Jenkins 点 Build Now。

架构总览:

```
浏览器 → Higress(ai.hengan.com/xiaoan-platform) → xiaoan-ui-service → xiaoan-ui(Nginx 容器)
    Nginx 内部路由:
      /xiaoan-platform/            → React 静态文件(SPA,刷新不 404)
      /xiaoan-platform/api/*       → xiaoan-api:8080
      /xiaoan-platform/api/v2/runs/<id>/stream → xiaoan-stream:8081(SSE,无 buffering)
      /xiaoan-platform/ws          → xiaoan-api:8080(WebSocket)
后端 5 个角色共用一个镜像 xiaoan-backend,靠 command 区分:
    api | stream | worker --provider=feishu_aily | worker --provider=feishu_delivery | scheduler
MySQL / Redis 均为外部服务;附件走 NFS 直挂(无 PV/PVC)。
```

## 文件清单

| 文件 | 用途 |
|---|---|
| `k8s/configmap.yaml` | 非敏感配置(生产值已填) |
| `k8s/secret.example.yaml` | Secret 模板(**复制**为 `k8s/secret.yaml` 填真实值,已被 gitignore) |
| `k8s/migrate-job.yaml` | 人工执行的一次性迁移 Job(填 tag 后 apply) |
| `k8s/xiaoan-api.yaml` | API Deployment(2 副本)+ Service:8080 |
| `k8s/xiaoan-stream.yaml` | SSE Deployment(2 副本)+ Service:8081 |
| `k8s/xiaoan-worker-aily.yaml` | Aily worker(1 副本,无 Service) |
| `k8s/xiaoan-worker-delivery.yaml` | Delivery worker(1 副本,无 Service) |
| `k8s/xiaoan-scheduler.yaml` | 调度器(1 副本,Recreate,无 Service) |
| `k8s/xiaoan-ui-proxy.yaml` | UI Deployment(2 副本)+ Service:80 |
| `k8s/ingress-higress.yaml` | Higress Ingress → xiaoan-ui-service:80 |
| `backend-go/Dockerfile` | 多阶段构建,产物含 api/stream/worker/scheduler/migrate + 迁移文件 |
| `frontend/Dockerfile` | 仅公司 Nginx 镜像 + dist + nginx.generated.conf(编译在 Jenkins) |
| `Jenkinsfile` | 全流程:编译 → 双镜像 → Harbor → set image → rollout 检查 |

关键设计约束(与参考文档一致):只用 2 个业务镜像;NFS 直挂不建 PV/PVC;MySQL/Redis 不进 K8s;**migration 一律人工执行**;tag 用 `test-YYYYMMDDHHMMSS-<sha>`(本次为 test 分支测试部署)。

---

## 一、前置条件检查

```bash
# 1. Namespace(不存在则建)
kubectl get ns eboat-ai-ns || kubectl create ns eboat-ai-ns

# 2. Harbor 拉取凭据(不存在则建;需 Harbor 项目 eboat2 的机器人账号)
kubectl -n eboat-ai-ns get secret harbor-secret || \
kubectl -n eboat-ai-ns create secret docker-registry harbor-secret \
  --docker-server=harbor.hengan.com:8086 \
  --docker-username=<HARBOR_USER> --docker-password=<HARBOR_PASSWORD>

# 3. K8s 节点能访问 NFS(Jenkins 或任一 node 上)
showmount -e 192.168.212.165        # 应列出 /db/k8s-ai-nfs

# 4. NFS 目录已建且容器可写(UID 10001)
# 在 NFS server 上:
mkdir -p /db/k8s-ai-nfs/xiaoan-platform-data-test
chown 10001:10001 /db/k8s-ai-nfs/xiaoan-platform-data-test
# 不要 chmod 777;若 NFS 使用 root_squash 需改为 no_root_squash 或相应 anonuid/anongid=10001

# 5. 外部 MySQL 5.7 / Redis 连通性(node 上验证)
mysql -h <DB_HOST> -u <DB_USER> -p -e "SELECT 1"
redis-cli -h <REDIS_HOST> -a <REDIS_PASSWORD> ping
```

**NFS 权限验证方法**:部署后如果 pod 起不来,events 出现 `Permission denied` 或 `access denied by server`,几乎都是 NFS 导出权限/UID 不匹配问题。

## 二、Secret 准备(一次性)

```bash
cp k8s/secret.example.yaml k8s/secret.yaml
# 编辑 k8s/secret.yaml,填入:
#   DB_HOST / DB_USER / DB_PASSWORD     — 外部 MySQL
#   REDIS_HOST / REDIS_PORT / REDIS_PASSWORD
#   FEISHU_APP_ID / FEISHU_APP_SECRET
#   TOKEN_ENCRYPTION_KEY                — openssl rand -base64 32 生成,妥善保存;
#                                          丢失=所有飞书登录态失效
kubectl apply -f k8s/secret.yaml
```

注意:`TOKEN_ENCRYPTION_KEY` 用于飞书 refresh token 落库加密,生成后要存档(密码库);后续所有环境复用同一个 key。

## 三、ConfigMap 部署(一次性)

```bash
kubectl apply -f k8s/configmap.yaml
```

发布前需确认两个值:
- `REDIS_MODE`:`standalone`(默认)还是 `cluster`,按公司生产 Redis 实际情况改
- `TRUSTED_PROXY_CIDRS`:默认 `127.0.0.1/32,::1/128,172.16.0.0/12`;如果 pod 网段不是 172.16.0.0/12,通过 `kubectl -n eboat-ai-ns get pod -o wide` 查看 xiaoan-ui pod IP 段后追加

## 四、数据库 Migration(人工执行)

优先用专门的 Job manifest(推荐,透明可控):

```bash
# 1. 把 k8s/migrate-job.yaml 里的 REPLACE_WITH_IMAGE_TAG 换成要发布的镜像 tag
# 2. 执行并观察日志
kubectl apply -f k8s/migrate-job.yaml
kubectl -n eboat-ai-ns logs job/xiaoan-migrate -f   # 期望看到 "migrations applied"
# 3. 清理(便于下次重跑)
kubectl -n eboat-ai-ns delete job xiaoan-migrate
```

Job 说明:`backoffLimit: 0`——迁移失败**不会**自动重试;命令是独立的 `migrate` 二进制(不是 `scheduler --migrate`,不是 `api -migrate`),从 `xiaoan-secret` 读 DB 连接信息,迁移文件在镜像 `/app/db/migrations`。

**若数据库此前已跑过旧版 0044 迁移**(seed 的内置 OCR 模型是启用状态,而当前版本 OCR 已下线),迁移后补一条 SQL 将其停用:

```sql
UPDATE ai_models SET enabled = FALSE
WHERE model_id = 'PP-OCRv6_tiny' AND execution_location = 'browser_local';
```

全新数据库不受影响(0044 已改为以停用状态 seed)。

等价做法(本地直连数据库):

```bash
cd backend-go && go build -o migrate.exe ./cmd/migrate
# 设好 DB_HOST/DB_USER/DB_PASSWORD/DB_NAME 环境变量后:
./migrate.exe -dir db/migrations
```

**严禁**在 Jenkinsfile、InitContainer、Deployment 启动参数里加任何 migrate 行为。

## 五、首次部署 Workload

首次部署时镜像还不存在(还没跑过 Jenkins)。推荐顺序(**migration 需要镜像先存在**,故顺序是:先出镜像 → 再 migration → 再起应用):

1. **Jenkins 首跑**:勾选 `SKIP_DEPLOY`(只构建 + 推送镜像,不碰 K8s)。完成后从控制台日志复制 `IMAGE_TAG=` 的值。
2. **人工执行 migration**(第四节):把 `k8s/migrate-job.yaml` 的占位 tag 换成第 1 步的 tag,apply 并确认日志出现 `migrations applied`。
3. **apply 全部 Workload**:`kubectl apply -f k8s/xiaoan-api.yaml ...`(命令如下)。YAML 里的 `REPLACE_WITH_IMAGE_TAG` 占位值此时可以不改——第 4 步 set image 会覆盖它;也可直接替换成真实 tag,两种都行。
4. **Jenkins 第二跑**:不勾 `SKIP_DEPLOY`,正常全流程(set image + rollout 检查)。rollout 通过即部署完成。

```bash
kubectl apply -f k8s/xiaoan-api.yaml
kubectl apply -f k8s/xiaoan-stream.yaml
kubectl apply -f k8s/xiaoan-worker-aily.yaml
kubectl apply -f k8s/xiaoan-worker-delivery.yaml
kubectl apply -f k8s/xiaoan-scheduler.yaml
kubectl apply -f k8s/xiaoan-ui-proxy.yaml
kubectl apply -f k8s/ingress-higress.yaml
```

之后日常发布:Jenkins 直接 Build Now(不勾 SKIP_DEPLOY),无需再改 YAML。

## 六、Jenkins 日常发布

1. Jenkins 新建 Pipeline 任务,源指向 GitLab `http://192.168.0.81/ai_sys/xiaoan-platform.git`,branch `test`
2. 调整 Jenkinsfile 里 `GIT_CREDENTIAL_ID`(Jenkins 凭据 ID),以及公司实际的 Harbor 登录方式
3. **首跑**勾选 `SKIP_DEPLOY`(只出镜像,为 migration 准备);日常发布不勾,直接 Build Now

Jenkins 会:nodedkbuild(node22140)npm ci → `APP_BASE_PATH=/xiaoan-platform/ NGINX_API_UPSTREAM=xiaoan-api:8080 NGINX_STREAM_UPSTREAM=xiaoan-stream:8081 npm run build` → docker build 两个镜像(同 tag)→ push Harbor → 6 个 Deployment set image → 逐个 rollout status(失败即 FAIL,不会静默成功)。

**OCR 已临时下线**(2026-09):构建不再联网下载模型、UI 镜像不含 OCR 资产。`npm run build` 全程离线可用。数据库 seed 的内置 OCR 模型为停用状态,UI 里"开始本地识别"按钮会提示组件下线。**恢复 OCR**:`npm run ocr:enable`(重新生成 runtime + 下载模型)→ 还原 `frontend/src/features/ai-models/OcrEntry.tsx` 中 `loadOcrPanel` 的动态 import(git 历史)→ 重新构建部署;详见 `frontend/scripts/ocr-deployment.test.mjs` 顶部注释与 `docs/ocr-browser-deployment.md`。

## 七、验证清单

```bash
kubectl -n eboat-ai-ns get pods    # 8 pods 全部 Running(api2/stream2/worker1/worker1/scheduler1/ui2)
kubectl -n eboat-ai-ns get ingress
```

- [ ] 浏览器打开 `https://ai.hengan.com/xiaoan-platform/`,登录正常(cookie Secure,必须走 HTTPS)
- [ ] 刷新 `/xiaoan-platform/tasks/<id>` 之类深链不 404(SPA fallback)
- [ ] 发起一次任务,SSE 输出逐字流式(不整段一次性出)
- [ ] 上传附件后另一个角色/另一个 pod 能读到(NFS 共享验证:`kubectl exec` 两个 api pod 互查 `/app/data/storage`)
- [ ] Worker 日志出现 `worker consuming provider=feishu_aily` / `feishu_delivery`
- [ ] scheduler 只有一个实例:`kubectl -n eboat-ai-ns get pods -l app=xiaoan-scheduler` 数量=1
- [ ] WebSocket:页面控制台无 WS 断连报错
- [ ] `kubectl -n eboat-ai-ns logs deploy/xiaoan-api --tail` 无 DB/Redis 连接错误

## 八、回滚

```bash
# 查历史 tag(Harbor 或 Jenkins 控制台),回滚到上一个正常 tag:
kubectl -n eboat-ai-ns set image deployment/xiaoan-api \
  xiaoan-api=harbor.hengan.com:8086/eboat2/xiaoan-backend:<旧tag>
# 5 个后端 deployment 同理换 <旧tag>;前端:
kubectl -n eboat-ai-ns set image deployment/xiaoan-ui \
  xiaoan-ui=harbor.hengan.com:8086/eboat2/xiaoan-ui:<旧tag>

# 或按 revision 回退:
kubectl -n eboat-ai-ns rollout undo deployment/xiaoan-api
```

回滚只需要旧镜像 tag,不重新编译。**不要**覆盖已推送的 tag。

## 九、常见排错

| 症状 | 排查 |
|---|---|
| Pod `ImagePullBackOff` | `kubectl -n eboat-ai-ns describe pod <p>`;查 harbor-secret 是否在 ns、Harbor 项目名/tag 是否正确 |
| Pod `CreateContainerConfigError` | ConfigMap/Secret 未 apply,或 key 名对不上(`kubectl describe` 会列出缺失 key) |
| API pod 起不来,log 报 `production config missing secrets: ...` | Secret 里缺 FEISHU_APP_SECRET / DB_PASSWORD / TOKEN_ENCRYPTION_KEY(`APP_ENV=production` 强制校验) |
| `/health/ready` 一直 503 | 后端连不上 MySQL/Redis:`kubectl exec` 进 pod 用 nc 验证 <DB_HOST>:3306 / <REDIS_HOST>:6379 |
| 附件上传报错/404 | NFS 挂载:`kubectl describe pod` 看 volume 事件;节点 `showmount -e 192.168.212.165` |
| SSE 无输出或整段输出 | Higress/Nginx buffering:确认响应头有 `X-Accel-Buffering: no`;Higress 若加缓冲需关闭 |
| 登录后又跳回登录页 | 非 HTTPS 访问(cookie Secure)或 TRUSTED_PROXY_CIDRS 不含实际代理 IP 段 |
| 深链刷新 404 | Ingress path /xiaoan-platform 未生效,或镜像里 nginx.generated.conf 生成时 APP_BASE_PATH 不对 |
| 飞书回调失败 | `PUBLIC_ORIGIN` 必须是 `https://ai.hengan.com`(不带路径);回调地址=PUBLIC_ORIGIN + basePath + /auth/feishu/callback |
