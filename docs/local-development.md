# 本地开发与验证

## 1. 前置条件

- Go 1.27 系列，仓库本机使用过 1.27.1；npm 和支持本项目构建的 Node（现有 Docker 使用 Node 24，Actions 使用 Node 22）。依赖安装使用 `npm ci`。
- 能访问配置的 MySQL/Redis、飞书及必要内部上游。
- 先按[数据库文档](deployment/database.md)人工完成数据库准备。不要为了启动服务反复执行整个 SQL 基线。
- 以下 PowerShell 命令在仓库根目录开始；Linux 用 `export STUDIO_ENV=dev`，可执行文件无 `.exe` 后缀。

## 2. 配置

本机已有 `backend-go/.env.development.local`、`.env.test.local`、`.env.production.local`。
从新克隆环境初始化时，可以将 `backend-go/.env.example` 复制为 `.env.development.local`，填真实配置；模板中的注释已独立成行；自行添加配置时也不得写行尾注释，当前后端解析器不会剥离它。
详见[配置优先级](environment-profiles.md)。本地服务使用 `STUDIO_ENV=dev`；Git 分支叫 test 不代表运行环境就是 test。

## 3. 构建与启动

```powershell
Set-Location backend-go
$env:STUDIO_ENV = 'dev'
# 仅在默认模块代理不可达时设置组织批准的 GOPROXY。
go version
go build -o bin/api.exe ./cmd/api
go build -o bin/stream.exe ./cmd/stream
go build -o bin/worker.exe ./cmd/worker
go build -o bin/scheduler.exe ./cmd/scheduler
```

分别打开 5 个终端，均切换到 `backend-go`，**每个终端**设置 `$env:STUDIO_ENV='dev'`，然后分别执行一行：

```powershell
.\bin\api.exe
.\bin\stream.exe
.\bin\worker.exe --provider=feishu_aily
.\bin\worker.exe --provider=feishu_delivery
.\bin\scheduler.exe
```

第 6 个终端，切换到 `frontend`：

```powershell
npm ci
npm run dev -- --port 3030 --strictPort
```

普通用户：`http://localhost:3030/xiaoan-platform/`；管理员：`http://localhost:3030/xiaoan-platform/login/admin`。
本机 Vite 可能仅监听 `::1`；使用 `localhost`，不要未经检查就换成 `127.0.0.1`。端口冲突先确认进程归属，不要批量杀死所有 node 或 scheduler。

`npm run dev` 只准备 OCR runtime；需要验证完整本地 OCR 模型时另执行 `npm run ocr:prepare`。

## 4. 配置生效

后端配置在启动时读取，修改 env 后重启所有后端角色。
前端 `STUDIO_ENV` 与 Vite mode 没有关联；`VITE_*` 浏览器变量在构建时注入。当前 `VITE_API_TARGET`/`VITE_WS_TARGET` 由 Vite 配置直接读取 `process.env`，**不能保证只放进 dotenv 文件就会改变代理**，需要启动前设置进程环境变量。

```powershell
# 仅在需要覆盖代理时，启动 Vite 的同一个终端执行。
$env:VITE_API_TARGET='http://localhost:8080'
$env:VITE_WS_TARGET='ws://localhost:8080'
npm run dev -- --port 3030 --strictPort
```

## 5. 验证

```powershell
Invoke-WebRequest http://localhost:8080/health/live
Invoke-WebRequest http://localhost:8080/health/ready
Invoke-WebRequest http://localhost:8081/health/ready
```

后端普通检查（在 `backend-go`，不启用真实库测试开关）：

```powershell
go build ./...
go vet ./...
go test ./... -count=1
```

Windows 如遇测试临时 exe 被拦截，应说明限制；可将相关测试包用 `go test -c -o bin/...test.exe` 构建后从项目目录执行。不能把被拦截等同测试通过，也不要关闭系统安全软件。

前端检查（在 `frontend`）：

```powershell
npm run typecheck
npm test
npm run test:deployment
npm run test:ocr-deployment
npm run build
```

**隔离集成测试**需要单独 MySQL/Redis、明确的 `STUDIO_TEST_ISOLATED=1` 等守卫；查看 `tests/integration/environment_guard_test.go` 和工作流。dev/test 目前共享业务资源，禁止把 `STUDIO_TEST_DB=1` 或 Redis 清库测试直接用于它们。真实发飞书、执行账号密码操作也不是普通启动验收的一部分。
