# xiaoan-platform 部署手册(K8s + Jenkins + Harbor + Higress)

本文档是**执行步骤**,按顺序走完即完成首次部署;之后日常发布只需在 Jenkins 点 Build Now。

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

K8s 清单按环境分目录,生产在 `eboat-ai-ns`、测试在 `test-ai-ns` 两个 namespace 并行运行(测试环境资源名全部带 `-test` 后缀):

| 目录 | 文件 | 用途 |
|---|---|---|
| `k8s/prod/` | `configmap.yaml` | 生产非敏感配置(值来自 backend-go/.env.production.local) |
| | `secret.example.yaml` | Secret 模板(**复制**为 `secret.yaml` 填真实值,已被 gitignore) |
| | `migrate-job.yaml` | 人工执行的一次性迁移 Job(填 tag 后 apply) |
| | `xiaoan-api.yaml` | API Deployment(2 副本)+ Service:8080 |
| | `xiaoan-stream.yaml` | SSE Deployment(2 副本)+ Service:8081 |
| | `xiaoan-worker-aily.yaml` | Aily worker(1 副本,无 Service) |
| | `xiaoan-worker-delivery.yaml` | Delivery worker(1 副本,无 Service) |
| | `xiaoan-scheduler.yaml` | 调度器(1 副本,Recreate,无 Service) |
| | `xiaoan-ui-proxy.yaml` | UI Deployment(2 副本)+ Service:80 |
| | `ingress-higress.yaml` | Higress Ingress → ai.hengan.com/xiaoan-platform |
| `k8s/test/` | 同上结构,资源名全部 `-test` 后缀 | 测试环境:独立 MySQL/Redis、NFS 目录、Ingress(ai-test.hengan.com) |
| 根目录 | `backend-go/Dockerfile` | 多阶段构建,产物含 api/stream/worker/scheduler/migrate + 迁移文件 |
| | `frontend/Dockerfile` | 仅公司 Nginx 镜像 + dist + nginx.generated.conf(编译在 Jenkins) |
| | `Jenkinsfile.backend` / `Jenkinsfile.backend-test` | 后端流水线:构建 xiaoan-backend 镜像 → 更新对应环境的 5 个后端 Deployment(prod/test 各一份,环境写死) |
| | `Jenkinsfile.frontend` / `Jenkinsfile.frontend-test` | 前端流水线:Node 编译 → 仅构建 xiaoan-ui 镜像 → 更新对应环境的 xiaoan-ui(prod/test 各一份) |
| | `Jenkinsfile.full` | 合并版备用:两个镜像一起构建发布(日常不用,一键全量重发时用;仅 prod) |

两环境差异速查:

| 项 | 生产 (prod) | 测试 (test) |
|---|---|---|
| **Namespace** | `eboat-ai-ns` | `test-ai-ns` |
| 资源名 | `xiaoan-api` 等 | `xiaoan-api-test` 等 |
| ConfigMap/Secret | `xiaoan-config` / `xiaoan-secret` | `xiaoan-config-test` / `xiaoan-secret-test` |
| MySQL | 192.168.212.165:23306/xiaoan(xiaoanuser) | 192.168.211.26:20336/xiaoan(test_user) |
| Redis | 集群 192.168.212.165:6381-6383(prefix xiaoan-platform) | standalone 192.168.211.26:6381 DB2(prefix xiaoan3) |
| NFS | /db/k8s-ai-nfs/xiaoan-platform-data | /db/k8s-ai-nfs/xiaoan-platform-data-test |
| Ingress | ai.hengan.com | ai-test.hengan.com(域名需先在 DNS/网关配置) |
| APP_ENV | production(cookie Secure) | test |
| Nginx upstream | xiaoan-api:8080 / xiaoan-stream:8081 | xiaoan-api-test:8080 / xiaoan-stream-test:8081 |
| 镜像 tag 前缀 | `prod-YYYYMMDDHHMMSS-<sha>` | `test-YYYYMMDDHHMMSS-<sha>` |

关键设计约束(与参考文档一致):只用 2 个业务镜像;NFS 直挂不建 PV/PVC;MySQL/Redis 不进 K8s;**migration 一律人工执行**;tag 用 `<env>-YYYYMMDDHHMMSS-<sha>`(前缀写在各环境流水线文件里:prod-/test-)。

---

## 一、前置条件检查

```bash
# 1. Namespace(不存在则建)
kubectl get ns eboat-ai-ns  || kubectl create ns eboat-ai-ns   # 生产
kubectl get ns test-ai-ns  || kubectl create ns test-ai-ns   # 测试

# 2. Harbor 拉取凭据(不存在则建;需 Harbor 项目 eboat2 的机器人账号;两个 namespace 各建一份)
for ns in eboat-ai-ns test-ai-ns; do
  kubectl -n $ns get secret harbor-secret || \
  kubectl -n $ns create secret docker-registry harbor-secret \
    --docker-server=harbor.hengan.com:8086 \
    --docker-username=<HARBOR_USER> --docker-password=<HARBOR_PASSWORD>
done

# 3. K8s 节点能访问 NFS(Jenkins 或任一 node 上)
showmount -e 192.168.212.165        # 应列出 /db/k8s-ai-nfs

# 4. NFS 目录已建且容器可写(UID 10001)
# 在 NFS server 上(两个环境的附件目录分开):
mkdir -p /db/k8s-ai-nfs/xiaoan-platform-data
chown 10001:10001 /db/k8s-ai-nfs/xiaoan-platform-data
mkdir -p /db/k8s-ai-nfs/xiaoan-platform-data-test
chown 10001:10001 /db/k8s-ai-nfs/xiaoan-platform-data-test
# 不要 chmod 777;若 NFS 使用 root_squash 需改为 no_root_squash 或相应 anonuid/anongid=10001

# 5. 外部 MySQL 5.7 / Redis 连通性(node 上验证)
mysql -h <DB_HOST> -u <DB_USER> -p -e "SELECT 1"
redis-cli -h <REDIS_HOST> -a <REDIS_PASSWORD> ping
```

**NFS 权限验证方法**:部署后如果 pod 起不来,events 出现 `Permission denied` 或 `access denied by server`,几乎都是 NFS 导出权限/UID 不匹配问题。

## 二、Secret / ConfigMap 准备(一次性)

真实 secret.yaml 已在 `k8s/prod/secret.yaml` 和 `k8s/test/secret.yaml` 填好真实值(gitignore,不入库)。模板见 `k8s/prod/secret.example.yaml`。重新生成时:

```bash
cp k8s/prod/secret.example.yaml k8s/prod/secret.yaml    # 生产
cp k8s/prod/secret.example.yaml k8s/test/secret.yaml    # 测试(名字须为 xiaoan-secret-test)
# 编辑填入: DB_HOST / DB_USER / DB_PASSWORD / REDIS_* / FEISHU_* / TOKEN_ENCRYPTION_KEY / BUSINESS_*
kubectl apply -f k8s/prod/secret.yaml
kubectl apply -f k8s/test/secret.yaml
```

注意:`TOKEN_ENCRYPTION_KEY` 用于飞书 refresh token 落库加密,生成后要存档(密码库);所有环境复用同一个 key(当前两环境值一致)。

## 三、ConfigMap 部署(一次性)

```bash
kubectl apply -f k8s/prod/configmap.yaml
kubectl apply -f k8s/test/configmap.yaml
```

发布前需确认:
- 测试环境域名 `ai-test.hengan.com` 已在公司 DNS/网关解析到 Higress(生产为 ai.hengan.com)
- `TRUSTED_PROXY_CIDRS`:默认 `127.0.0.1/32,::1/128,172.16.0.0/12`;如果 pod 网段不是 172.16.0.0/12,通过 `kubectl -n eboat-ai-ns get pod -o wide` 查看 xiaoan-ui pod IP 段后追加

## 四、数据库 Migration(人工执行)

优先用专门的 Job manifest(推荐,透明可控;两环境各有独立 Job):

```bash
# 1. 把 k8s/prod/migrate-job.yaml(或 k8s/test/migrate-job.yaml)里的
#    REPLACE_WITH_IMAGE_TAG 换成要发布的镜像 tag
# 2. 执行并观察日志
kubectl apply -f k8s/prod/migrate-job.yaml
kubectl -n eboat-ai-ns logs job/xiaoan-migrate -f      # 期望看到 "migrations applied"
# 测试环境:
kubectl apply -f k8s/test/migrate-job.yaml
kubectl -n test-ai-ns logs job/xiaoan-migrate-test -f
# 3. 清理(便于下次重跑)
kubectl -n eboat-ai-ns delete job xiaoan-migrate
kubectl -n test-ai-ns delete job xiaoan-migrate-test
```

Job 说明:`backoffLimit: 0`——迁移失败**不会**自动重试;命令是独立的 `migrate` 二进制(不是 `scheduler --migrate`,不是 `api -migrate`),从对应环境的 secret 读 DB 连接信息,迁移文件在镜像 `/app/db/migrations`。

等价做法(本地直连数据库):

```bash
cd backend-go && go build -o migrate.exe ./cmd/migrate
# 设好 DB_HOST/DB_USER/DB_PASSWORD/DB_NAME 环境变量后:
./migrate.exe -dir db/migrations
```

**严禁**在 Jenkinsfile、InitContainer、Deployment 启动参数里加任何 migrate 行为。

## 五、首次部署 Workload

首次部署时镜像还不存在(还没跑过 Jenkins)。推荐顺序(**migration 需要镜像先存在**,故顺序是:先出镜像 → 再 migration → 再起应用):

1. **Jenkins 首跑**:在对应环境的流水线任务上勾选 `SKIP_DEPLOY`(只构建 + 推送镜像,不碰 K8s)。完成后从控制台日志复制 `IMAGE_TAG=` 的值。
2. **人工执行 migration**(第四节):把对应环境 `migrate-job.yaml` 的占位 tag 换成第 1 步的 tag,apply 并确认日志出现 `migrations applied`。
3. **apply 全部 Workload**:`kubectl apply -f k8s/prod/...` 或 `k8s/test/...`(命令如下)。YAML 里的 `REPLACE_WITH_IMAGE_TAG` 占位值此时可以不改——第 4 步 set image 会覆盖它;也可直接替换成真实 tag,两种都行。
4. **Jenkins 第二跑**:不勾 `SKIP_DEPLOY`,正常全流程(set image + rollout 检查)。rollout 通过即部署完成。

```bash
# 生产(k8s/prod/):
kubectl apply -f k8s/prod/xiaoan-api.yaml
kubectl apply -f k8s/prod/xiaoan-stream.yaml
kubectl apply -f k8s/prod/xiaoan-worker-aily.yaml
kubectl apply -f k8s/prod/xiaoan-worker-delivery.yaml
kubectl apply -f k8s/prod/xiaoan-scheduler.yaml
kubectl apply -f k8s/prod/xiaoan-ui-proxy.yaml
kubectl apply -f k8s/prod/ingress-higress.yaml

# 测试(k8s/test/,资源名全部 -test 后缀):
kubectl apply -f k8s/test/xiaoan-api.yaml
kubectl apply -f k8s/test/xiaoan-stream.yaml
kubectl apply -f k8s/test/xiaoan-worker-aily.yaml
kubectl apply -f k8s/test/xiaoan-worker-delivery.yaml
kubectl apply -f k8s/test/xiaoan-scheduler.yaml
kubectl apply -f k8s/test/xiaoan-ui-proxy.yaml
kubectl apply -f k8s/test/ingress-higress.yaml
```

之后日常发布:Jenkins 直接 Build Now(不勾 SKIP_DEPLOY),无需再改 YAML。

## 六、Jenkins 日常发布

流水线按 **环境写死**:每种流水线有 prod/test 两份文件,环境由 Jenkins 任务的 Script Path 决定,**没有环境参数可选**——prod 任务永远发 prod,test 任务永远发 test,不存在误选。

| 任务名(建议) | Script Path | 环境 |
|---|---|---|
| `xiaoan-backend` | `Jenkinsfile.backend` | prod(eboat-ai-ns) |
| `xiaoan-backend-test` | `Jenkinsfile.backend-test` | test(test-ai-ns) |
| `xiaoan-ui` | `Jenkinsfile.frontend` | prod(eboat-ai-ns) |
| `xiaoan-ui-test` | `Jenkinsfile.frontend-test` | test(test-ai-ns) |

另有合并版 `Jenkinsfile.full` 备用(仅 prod,一键全量重发)。镜像 tag 前缀写在各文件里(prod-/test-),回滚各查各的 tag。

1. Jenkins 按 上表 新建 **4 个 Pipeline 任务**,源都指向 GitLab `http://192.168.0.81/ai_sys/xiaoan-platform.git`,branch `test`
2. 调整各 Jenkinsfile 里 `GIT_CREDENTIAL_ID`(Jenkins 凭据 ID),以及公司实际的 Harbor 登录方式
3. **首跑**勾选 `SKIP_DEPLOY`(只出镜像,为 migration 准备);日常发布不勾,直接 Build Now

**后端流水线**:docker build xiaoan-backend → push → 更新本环境的 5 个后端 Deployment(prod 无后缀 / test 带 `-test`)→ 逐个 rollout status(不跑前端 Node 编译,发布明显更快)。

**前端流水线**:nodedkbuild(node22140)npm ci → `APP_BASE_PATH=/xiaoan-platform/ NGINX_API_UPSTREAM=<该环境的upstream> NGINX_STREAM_UPSTREAM=<该环境的upstream> npm run build` → docker build xiaoan-ui → push → 更新本环境的 xiaoan-ui → rollout status。⚠️ **Nginx upstream 烧在镜像里**,prod 与 test 的 xiaoan-ui 镜像不可混用(两份流水线文件已分别写死)。

**发布顺序约定**:当前后端有 breaking 改动时(API 字段/协议变更),**先发后端**(保持向后兼容),再发前端;平时改哪端发哪条流水线即可。上生产前建议先发 test 环境验证。

**OCR 模型说明**:前端 build 的 prebuild 钩子从 bcebos.com 下载 6.1MB OCR 模型(sha256 固定)。首次构建需公网;之后 `frontend/public/ocr-assets/ppocrv6-tiny-20260921/` 有缓存则完全离线。若 Jenkins 无公网:把任意已成功构建过的机器上的该目录打包上传到 Jenkins workspace 同路径即可。构建 fail-closed,缺模型会直接报错而不是静默缺功能。

## 七、验证清单

```bash
kubectl -n eboat-ai-ns get pods    # 生产 8 pods 全部 Running
kubectl -n test-ai-ns get pods     # 测试 8 pods 全部 Running
kubectl get ingress -A | grep xiaoan   # ai.hengan.com(prod) 与 ai-test.hengan.com(test)
```

生产:
- [ ] 浏览器打开 `https://ai.hengan.com/xiaoan-platform/`,登录正常(cookie Secure,必须走 HTTPS)
- [ ] 刷新 `/xiaoan-platform/tasks/<id>` 之类深链不 404(SPA fallback)
- [ ] 发起一次任务,SSE 输出逐字流式(不整段一次性出)
- [ ] 上传附件后另一个角色/另一个 pod 能读到(NFS 共享验证:`kubectl exec` 两个 api pod 互查 `/app/data/storage`)
- [ ] Worker 日志出现 `worker consuming provider=feishu_aily` / `feishu_delivery`
- [ ] scheduler 只有一个实例:`kubectl -n eboat-ai-ns get pods -l app=xiaoan-scheduler` 数量=1
- [ ] WebSocket:页面控制台无 WS 断连报错
- [ ] `kubectl -n eboat-ai-ns logs deploy/xiaoan-api --tail` 无 DB/Redis 连接错误

测试:同上,把 namespace 换成 `test-ai-ns`、资源名换成 `-test` 后缀、URL 换成 `http(s)://ai-test.hengan.com/xiaoan-platform/`、scheduler 查 `-l app=xiaoan-scheduler-test`。

## 八、回滚

前后端流水线 tag 各自独立,回滚时在对应环境的 Jenkins 任务(或 Harbor tag 前缀 prod-/test-)查各自的 `<旧tag>`:

```bash
# 后端回滚(测试环境资源名加 -test 后缀):查 xiaoan-backend 任务的历史 tag,
# 5 个后端 deployment 换成同一个旧 tag:
kubectl -n eboat-ai-ns set image deployment/xiaoan-api \
  xiaoan-api=harbor.hengan.com:8086/eboat2/xiaoan-backend:<后端旧tag>
# stream/worker-aily/worker-delivery/scheduler 同理换 <后端旧tag>

# 前端回滚(注意: prod 与 test 的 xiaoan-ui 镜像 upstream 不同,不可跨环境回滚):
kubectl -n eboat-ai-ns set image deployment/xiaoan-ui \
  xiaoan-ui=harbor.hengan.com:8086/eboat2/xiaoan-ui:<前端旧tag>

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
| 飞书回调失败 | `PUBLIC_ORIGIN` 必须是 `https://ai.hengan.com`(不带路径);回调地址=PUBLIC_ORIGIN + basePath + /auth/feishu/callback。⚠️ 两环境共用同一飞书应用,回调只会打到一个 PUBLIC_ORIGIN——飞书网页登录回调按环境手动切换,或后续为 test 环境申请独立飞书应用 |
