# Docker 部署手册（CentOS 7.9 应用主机）

本方案是**应用容器部署**，使用外部MySQL和Redis，不在宿主安装Go/Node，不创建数据库容器，不自动迁移数据库。
必须先完成[平台检查](platform-prerequisites.md)和[人工数据库操作](database.md)。CentOS7.9已结束维护；没有经过批准的Docker/Compose运行环境时，不继续上线。

## 0. 文件与替换参数

- [后端Dockerfile](../../deploy/docker/Dockerfile.backend)：构建上下文为backend-go。
- [前端Dockerfile](../../frontend/Dockerfile)：构建上下文为仓库根目录，读取deployment.json。
- [Compose](../../deploy/docker/compose.yaml)：六个服务，外部env，共享存储，无migration服务。
- [TLS Nginx](../../deploy/docker/nginx-tls.conf)：443，/xiaoan-platform/，SSE独立转发。
- [Compose变量模板](../../deploy/docker/compose.env.example)和[后端变量模板](../../deploy/backend.env.example)。

以下示例假设镜像仓库harbor.hengan.com:8086/eboat2，正式域名、证书、仓库权限均由平台确认。
REPLACE_*必须替换；不能把本地localhost回调直接用于生产。

## 1. 在受维护的构建机准备不可变版本

以**选定的发布提交**为源，不从浮动分支边拉边发布。当前分支名test只是Git分支，不是运行环境变量。

```bash
# 在仓库根目录；先确认工作区无未审阅改动。
git status --short
git rev-parse HEAD
export RELEASE=$(git rev-parse --short=12 HEAD)
export BACKEND_IMAGE=harbor.hengan.com:8086/eboat2/xiaoan-backend:$RELEASE
export WEB_IMAGE=harbor.hengan.com:8086/eboat2/xiaoan-web:$RELEASE
docker login harbor.hengan.com:8086
# 交互输入凭据；不要在命令中写 -p 明文密码。
docker build --pull -f deploy/docker/Dockerfile.backend -t "$BACKEND_IMAGE" backend-go
docker build --pull -f frontend/Dockerfile -t "$WEB_IMAGE" .
docker push "$BACKEND_IMAGE"
docker push "$WEB_IMAGE"
docker image inspect "$BACKEND_IMAGE" --format '{{.Architecture}} {{json .RepoDigests}}'
docker image inspect "$WEB_IMAGE" --format '{{.Architecture}} {{json .RepoDigests}}'
```

构建机与目标节点架构应一致；不同架构请使用已配置的buildx明确platform并验证，不把amd64镜像交给arm64节点。
基础镜像tag是可读默认值（后端Go1.27.1/Alpine3.22，前端Node24/Nginx1.27）；上线需镜像扫描并在发布记录锁定digest，不能把文档默认tag当安全更新承诺。后端可用GO_IMAGE/RUNTIME_IMAGE构建参数替换组织批准的镜像。

后端Dockerfile不复制dotenv/可变数据；前端根.dockerignore排除真实env。OCR构建会下载并校验固定资源；隔离网络需按[OCR文档](../ocr-browser-deployment.md)预置资源或提供批准出口，不跳过校验。

没有仓库网络时，可在构建机docker save两镜像为离线包，传输SHA256并核验后在主机docker load；不要把凭据文件放入包。

## 2. 在CentOS主机准备发布目录、数据和配置

将该发布提交的deploy/docker目录放在 /opt/xiaoan/releases/<RELEASE>/deploy/docker，保留同一版本的SQL工件和发布记录。
以下以仓库已安全解包到该release目录为例：

```bash
export RELEASE=REPLACE_RELEASE
cd /opt/xiaoan/releases/$RELEASE
sudo install -d -m 0750 /etc/xiaoan /etc/xiaoan/tls
sudo install -d -m 0750 -o 10001 -g 10001 /srv/xiaoan/storage
sudo install -m 0600 deploy/backend.env.example /etc/xiaoan/backend.env
sudo install -m 0600 deploy/docker/compose.env.example /etc/xiaoan/compose.env
sudoedit /etc/xiaoan/backend.env
sudoedit /etc/xiaoan/compose.env
```

在backend.env填DB/Redis/飞书/原有加密密钥，PUBLIC_ORIGIN填正式https域名；STUDIO_ENV=prod。
compose.env只填镜像tag和绝对路径，不放业务密码。模板固定localfs及/app/data/storage，所有后端共享同一个主机目录。
保留原存储数据；已有上传文件应停写、备份并迁移后再挂载，空目录会导致头像/附件404。

- plain KEY=value，无包裹引号、无行尾注释、无变量展开；Compose使用raw env_file，不会替换密码中的美元符号。
- 配置文件0600由负责发布的受控账号读取，别改成全员可读来解决权限问题；Docker操作权限本身应严格授权。
- TRUSTED_PROXY_CIDRS按实际Docker网段中受控web代理配置；不直接信任全网。
- 同库加密密钥必须一致，升级不要重新生成。

## 3. 安装TLS证书

把正式域名的证书链和私钥通过安全渠道放到：

```bash
/etc/xiaoan/tls/fullchain.pem
/etc/xiaoan/tls/privkey.pem
```

上面是文件路径，不是可执行命令。私钥仅root/部署管理员可读，证书覆盖PUBLIC_ORIGIN域名。
443经组织防火墙/负载均衡放行。模板不发布80，不提供HTTP登录；不要为测试方便将生产APP_ENV改成development。
飞书后台登记：https://正式域名/xiaoan-platform/auth/feishu/callback。

## 4. 审核配置与人工SQL

本机prod dotenv只是配置起点，不能整份复制后遗漏正式域名和共享存储。
在启动六个服务前完成[数据库手册](database.md)；首次库也一样。Compose没有自动建表服务，up不会代你迁移。

```bash
cd /opt/xiaoan/releases/$RELEASE
# 仅检查结构，不输出包含真实值的完整config。
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml config --quiet
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml pull
```

## 5. 启动与检查

先确认不存在仍运行的旧生产Worker/Scheduler，防止旧版本在新SQL上消费。共享库历史pending/定时计划须评估，启动可能触发真实飞书投递。

```bash
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml up -d
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml ps
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml exec -T api wget -qO- http://127.0.0.1:8080/health/ready
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml exec -T stream wget -qO- http://127.0.0.1:8081/health/ready
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml exec -T web nginx -t
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml logs --tail=100 worker-aily worker-delivery scheduler
curl --fail https://REPLACE_APP_DOMAIN/xiaoan-platform/ -o /dev/null
```

Redis5应出现一次fallback enabled而非持续unknown command。Docker healthcheck失败本身不等于容器自动重启；由运维告警处理。
Nginx会在启动时解析后端DNS；独立替换api/stream容器后，为防旧IP残留应重启web。业务验收按[运维手册](../operations.md)，不能只看healthy。

## 6. 升级（人工维护窗口）

1. 记录旧镜像digest/配置/SQL版本/存储快照，准备回滚工件。
2. 入口维护，停止Scheduler和Worker，按业务需要停API/Stream；确认无写入后备份。
3. DBA人工执行新增SQL，核验version和dirty；没有SQL差异则跳过。
4. 切到新release目录，更新compose.env镜像tag；config --quiet和pull通过后up -d。
5. 配置文件变化时用force-recreate确保注入新值，不能只restart保留旧环境。


```bash
# 在旧release目录停止同一Compose项目；不删除任何数据卷。
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml stop
# 人工SQL成功后，在新release目录：
docker compose --env-file /etc/xiaoan/compose.env -f deploy/docker/compose.yaml up -d --force-recreate
```

Compose项目名固定xiaoan。不要down -v，不删除/srv/xiaoan/storage，不清空Redis。

## 7. 回滚

仅当旧代码兼容当前schema时，恢复旧镜像tag和已匹配的配置，再up -d --force-recreate。数据库回滚不是“应用换回旧tag”自动完成；如有破坏性DDL，按DBA恢复方案处理数据、文件和密钥。

## 8. 验证边界

本仓库提供的是完整可编辑模板。当前文档验证包含静态结构/路径核对，不代表已在CentOS7.9构建、运行镜像、接通TLS或完成生产DB/Redis/飞书业务验收；这些必须在目标环境逐项签收。
