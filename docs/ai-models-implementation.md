# AI 模型管理与测试台

当前入口：企业管理 → 平台管理 → AI模型管理。实现已在当前仓库，不再以历史model分支名判断是否合并。
这是**模型基础设施和独立测试台**，不等于正式Run路由、业务查询或自动化已经接入模型。

## 功能与权限

- 管理远程连接、模型、能力与浏览器模型manifest；连接密钥写入后不可回读。
- OpenAI Chat与Gemini协议的能力按适配器和模型声明共同约束；不能只打开界面开关就认为供应商支持图片/PDF/视频。
- 测试会话/附件归当前用户，调用可能产生供应商费用。管理员也不能任意读取他人测试内容。
- 前端轮询服务端调用快照，不是浏览器直接访问供应商，也不是额外的Run SSE协议。
- ai.model.read/write、ai.connection.secret.write、ai.model.test、ai.model.log.read分别控制权限。

## 数据与部署

结构由0043_ai_models迁移定义，0044_ai_model_presets提供GLM/OCR无密钥预设。所有SQL按[人工数据库操作](deployment/database.md)执行。
已有数据库的TOKEN_ENCRYPTION_KEY不能随发版改变；凭据、对象存储、数据库要配套备份。
确需访问内部模型服务时用AI_MODEL_ALLOWED_HOSTS逐主机允许，普通远程服务仍遵守出站策略。白名单可能同时允许HTTP，禁止通配符。
模型管理附件文件上限20MiB，multipart请求需21MiB网关空间；长请求需核对每层timeout。
默认部署API1副本。模型测试运行器有进程内执行/取消状态，扩容前需验证跨Pod快照、停止与故障处理，不能只按无状态REST推断。

## OCR与配置

浏览器OCR图片不上传服务器/供应商，远程测试附件则会上传所选服务。OCR仅测试不调用正式条码业务接口。
参见[OCR资源](ocr-browser-deployment.md)及[GLM/OCR操作](glm-flashx-and-ocr-setup.md)。

## 验证边界

不沿用历史个人工作树截图和旧测试数字作为现版本保证。每次发布重新验证当前浏览器、网关、能力声明、供应商、取消、权限及存储行为。
