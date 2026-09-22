# dev / test / prod 环境配置

## 现在使用的文件

| 选择器 | 维护文件 | 当前约定 |
| --- | --- | --- |
| `STUDIO_ENV=dev` | `backend-go/.env.development.local` | 开发环境 |
| `STUDIO_ENV=test` | `backend-go/.env.test.local` | 除 APP_ENV 外与开发配置相同 |
| `STUDIO_ENV=prod` | `backend-go/.env.production.local` | 生产连接配置；不是无需审核即可上线的配置 |

真实文件被 Git 忽略。新机无法从 Git 获取密码，应通过密钥管理渠道分发。本机已移除通用 `.env.local`；部署也不再创建它。

当前记录的连接目标（不含凭据）：

| 环境 | MySQL | Redis |
| --- | --- | --- |
| dev/test | `192.168.211.26:20336` / `xiaoan` | standalone `192.168.211.26:6381` / DB 2 |
| prod | `192.168.212.165:23306` / `xiaoan` | Cluster `192.168.212.165:6381,6382,6383` / DB 0 |

dev/test 共用实例、库与前缀意味着会共享 Session、任务和消费组；这是当前明确配置，不是测试隔离。生产必须与开发实例/库分开，同一环境内的所有角色使用相同 Redis 前缀。新模板建议xiaoan-prod；已有部署应保留原前缀，不能无迁移计划直接替换。

## 实际加载规则（未改动代码）

优先选择**进程变量** `STUDIO_ENV`，其次进程变量 `APP_ENV`，否则默认 development。
显式选择器决定运行环境；不要在文件中写一个选择器就期待程序先读那个文件。

从高到低：进程环境变量 > 下表文件顺序 > 代码默认值。

| profile | 文件顺序 |
| --- | --- |
| development | `.env.development.local` → `.env.local` → `.env.development` → `.env` |
| test | `.env.test.local` → `.env.development.local` → `.env.local` → `.env.test` → `.env.development` → `.env` |
| production | `.env.production.local` → `.env.local` → `.env.production` → `.env` |

对每个文件名，加载器从工作目录向上查最多 4 层父目录，先找到的一份生效；后续文件只补未设置键。
**代码仍兼容旧 `.env.local`，本次是停用文件而非删除加载功能。** test 会回退开发文件，因此正式隔离测试必须给完整配置，不能靠缺文件来阻止误连。

格式为独立 `KEY=value`，注释单独一行。后端不做 shell 变量展开，不支持 `export KEY=...`、行尾注释或多行值；不要把 `$PASSWORD` 当占位引用。
推荐使用无包裹引号的普通值，让后端、Compose raw env_file、Kubernetes `--from-env-file` 三者保持一致。

## 生产必审配置

- `PUBLIC_ORIGIN=https://正式域名`：只含 origin，无子路径。`APP_BASE_PATH=/xiaoan-platform/` 与前端构建一致。
- `FEISHU_APP_ID/FEISHU_APP_SECRET`：配置对应组织授权；后台登记完整回调路径。
- `TOKEN_ENCRYPTION_KEY`：同库同密钥，已有数据必须保留原密钥；新空库才可生成新值。
- `DB_*`：生产 runtime 建议专用最小权限用户。已有本地 prod 文件使用 root 不代表模板鼓励 root 常驻。
- `REDIS_DATABASE=0`、`REDIS_MODE=cluster`、完整节点地址；所有被集群发布的节点从容器网络可达。
- `STORAGE_*`：共享持久化目录或 S3。镜像和 Pod 临时文件系统不存唯一数据。
- `TRUSTED_PROXY_CIDRS`：仅填实际受控代理源网段，不能全网信任；不能想当然使用开发默认值。
- `ENTERPRISE_RBAC_ENABLED` / `ENTERPRISE_ACL_ENABLED`：基于已审核的角色/授权切换，不随发版自动开启新权限策略。
- `AI_MODEL_ALLOWED_HOSTS`：仅放模型上游所需的精确内部主机，不能用通配符绕过出站保护。

`ADMIN_BOOTSTRAP_USERNAME/PASSWORD` 当前只有配置字段，没有自动创建管理员的调用。`METRICS_ADDR` 也没有独立监听服务。不要根据变量名推断已有功能。

## Docker 与 Kubernetes

使用 [deploy/backend.env.example](../deploy/backend.env.example) 准备完整配置；它只有占位符。
Docker 通过外部 env_file 注入，Kubernetes 通过 Secret 注入，不把本机 dotenv 复制进镜像。模板中的 raw env_file 要求 Compose 2.30+。
不要输出包含真实值的 `docker compose config`、Secret YAML 或容器环境到共享日志；验证用 `config --quiet`。

## 前端与子路径

前端不读取后端文件。`VITE_API_BASE_URL` 留空即同源；所有 `VITE_*` 都不应包含秘密。
当前开发 `.env.development` 存在但未被 Git 跟踪；不依赖它也可使用默认同源行为。
`VITE_API_TARGET/VITE_WS_TARGET` 覆盖方式见[本地开发](local-development.md)。路径配置详见[子路径与代理](deployment-subpath.md)。
