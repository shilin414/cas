# 浏览器端 OCR 部署与实测说明

核验时间：2026-09-21（本机 Asia/Shanghai）。本说明只覆盖浏览器本地 OCR，不代表飞书、iOS、Android 真机认证。

## 1. 实际选型，不是占位 SDK

- SDK：`@paddleocr/paddleocr-js@0.4.2`，PaddlePaddle 官方项目的 npm 包。
- 模型：官方 **PP-OCRv6_tiny_det + PP-OCRv6_tiny_rec** ONNX 未压缩 tar，没有静默改用 v5、其他厂商模型或远程识别。
- ORT：`onnxruntime-web@1.22.0`，选择纯 WASM 构建，单线程，禁用代理 Worker 与 WebGPU 推理。
- OpenCV：`@techstark/opencv-js@4.10.0-release.1`，固定在应用构建中。
- 模型参数量 **1.5M 不是下载包 1.5MB**。实际字节如下。

### 固定发布资源

版本：`ppocrv6-tiny-20260921`。名称必须精确为表中两项。

| 资源 | 原始字节 | SHA-256 |
|---|---:|---|
| PP-OCRv6_tiny_det.tar | 1,792,000 | `ff6ab415b0a6e0c488550f2fb5d5046f1719848df220b2dc21b56402a65bc05d` |
| PP-OCRv6_tiny_rec.tar | 4,526,080 | `1e13b22717b1edd89d4cde4fda272b6c17d5b505c97c2baea99da1a3a2d54b29` |
| **模型合计** | **6,318,080** | 约 6.03 MiB，包含 ONNX + YAML/词表和 tar 开销 |
| 本次构建 ocr.worker.js | 10,536,772 | 构建时生成，Vite 输出内容哈希文件名；包括 SDK、OpenCV（内嵌其 WASM）、ORT JS |
| ORT WASM | 11,210,254 | 来自上述固定 ORT npm 包 |
| ORT 配套 ESM（以 .js 发布） | 20,856 | 来自同一 ORT 版本，与 WASM 必须配套 |

后三项不是模型权重；以上主要资源总计 **28,085,962 字节**（约 26.78 MiB），还未计面板、React 等共享应用代码。HTTP 压缩后的实际流量取决于服务器配置，不能把源码包体积或模型参数量当成下载流量。更新源代码会改变 Worker 大小与输出哈希。

### 官方一手依据

核验时官方仓库 main 提交为 `dab3fe35379033fdcb2d0e9572fac0b36c9a9ebf`，避免依赖后续变化的 main 内容：

- [官方 SDK README](https://github.com/PaddlePaddle/PaddleOCR/blob/dab3fe35379033fdcb2d0e9572fac0b36c9a9ebf/paddleocr-js/packages/core/README.md)：真实 `PaddleOCR.create` / `predict`、tiny 显式模型名称、tar 格式。
- [官方模型资源映射](https://github.com/PaddlePaddle/PaddleOCR/blob/dab3fe35379033fdcb2d0e9572fac0b36c9a9ebf/paddleocr-js/packages/core/src/resources/model-asset.ts)：本脚本下载地址的来源。
- [官方 PP-OCRv6 算法说明](https://github.com/PaddlePaddle/PaddleOCR/blob/dab3fe35379033fdcb2d0e9572fac0b36c9a9ebf/docs/version3.x/algorithm/PP-OCRv6/PP-OCRv6.md)。
- [ORT Web 官方环境选项](https://onnxruntime.ai/docs/tutorials/web/env-flags-and-session-options.html)。
- 实际安装包的 `dist/*.d.ts` 与实现也已核对，不按宣传示例臆造 API。npm integrity 记录在 package-lock.json。

SDK 本身为 Apache-2.0；模型分发使用 PaddleOCR 项目许可说明，重新分发者仍应检查其使用场景与上游 NOTICE。OpenCV、ORT 等第三方许可随其依赖和构建保留。

## 2. 可复现准备与部署

在 `frontend` 工作目录执行（协作开发中依赖安装只能由约定的安装负责人执行）：

```powershell
npm ci
npm run build
```

- 标准 `npm run build`（包括 Dockerfile 中已有的 `RUN npm run build`）自动执行 `prebuild = npm run ocr:runtime && npm run ocr:prepare`，无需在构建前手动下载模型。成功的 `dist/ocr-assets/<version>/` 必须包含两份 tar、manifest.json 和 provenance.json。`npm run ocr:prepare` 仍可单独用于诊断。
- `ocr:prepare` 从官方 BCE BOS 地址下载**真实模型**，逐包检查固定长度和 SHA-256；上游内容变化会明确失败，不自动接受新 hash，不换模型。
- 输出 `frontend/public/ocr-assets/ppocrv6-tiny-20260921/manifest.json`，可完整复制到模型管理 UI 的 `browser_manifest` JSON 输入。
- 同目录 `provenance.json` 记录来源、提交与依赖版本；tar 文件只作为部署资产。
- `public/ocr-assets/.gitignore` 排除所有生成内容（仅 ignore 文件本身可入库）。**不要 `git add -f` 模型文件或 dist。** 已下载模型、运行截图和网络证据均不应提交为业务源码。
- `predev` 仍只运行 `ocr:runtime`，不下载模型，不执行识别。生产 `prebuild` 则额外准备模型；构建时下载不等于浏览器提前加载，浏览器仍只有用户开始识别才请求模型/引擎。直接绕过 npm 调用 `vite build` 时必须先执行这两个准备步骤，不应作为标准部署流程。
- 已有 `public/ocr-assets/<version>/` 缓存只有在固定长度和 SHA-256 都匹配时才会完全离线复用。缓存缺失/损坏且官方源不可达时准备脚本非零退出，标准 build 随之失败，不能产出/宣称新的可部署包。离线 Docker 构建需提前将经过校验的该目录纳入构建上下文；现有 `.dockerignore` 不排除它。不要把忽略的权重提交 Git。
- 生成运行资源放在 OCR 子目录的 `runtime/`，该目录除 `.gitignore` 外均忽略。它们是可重建构建输入，不是需要提交的源码。应用 build 后由 Vite 放入带哈希的 `dist/assets/`。
- `ocr:prepare` 使用现有 `deployment.json` / `APP_BASE_PATH`。若切换基础路径，先用同一配置重新生成 manifest，再 build，并更新 UI 中保存的 manifest。现有默认路径为 `/xiaoan-platform/`，不是写死根域 `/`。

例如当前 manifest（真实、可直接使用）：

```json
{
  "adapter": "paddleocr_tiny",
  "version": "ppocrv6-tiny-20260921",
  "resources": [
    {
      "name": "PP-OCRv6_tiny_det",
      "url": "/xiaoan-platform/ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_det.tar",
      "sha256": "ff6ab415b0a6e0c488550f2fb5d5046f1719848df220b2dc21b56402a65bc05d",
      "size_bytes": 1792000
    },
    {
      "name": "PP-OCRv6_tiny_rec",
      "url": "/xiaoan-platform/ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_rec.tar",
      "sha256": "1e13b22717b1edd89d4cde4fda272b6c17d5b505c97c2baea99da1a3a2d54b29",
      "size_bytes": 4526080
    }
  ]
}
```

模型记录必须为 `model_id: PP-OCRv6_tiny`、`capability_kind: ocr`、`execution_location: browser_local`、启用状态。未知模型明确禁用开始识别按钮，不会偷偷替换。

### 静态资源服务器

- 在与页面相同的源上部署 `BASE/ocr-assets/<version>/<name>.tar`；禁止重定向到对象存储外域或带鉴权跳转。
- `.wasm` 使用 `application/wasm`；应用发布的配套 ESM 使用 `.js` 后缀，走既有 JavaScript MIME；tar 使用 `application/octet-stream`。上游 `ort-wasm-simd-threaded.mjs` 字节原样复制为 `ort-wasm-simd-threaded.js`，运行时 URL 与 Worker 请求类型同步为 moduleUrl；ORT 自身 wasmPaths 的官方字段名仍是 mjs。**不要求为全站新增 .mjs MIME 或放宽 CSP**。缺失资源返回真正 404，不要落入 SPA 的 index.html fallback（该 Nginx 配置由主集成负责）。
- 模型 URL 不接收查询串、fragment、编码/相对穿越、账号口令、其他资源名；每包最大 64 MiB。
- 浏览器资源下载 `credentials: omit`、同源、拒绝 redirect，分块累计大小上限，完整 SHA-256 后才交给 SDK。
- 模型版本目录须不可变。升级模型需新版本、重新记录 hash、样本验证后显式切换配置；旧客户端公开模型缓存不能被服务端即时撤销。
- 模型缓存键含版本和全部内容 hash；命中后**再次校验**。损坏缓存会删除并重新下载；拒绝/耗尽 CacheStorage 时仍可在内存中识别。面板提供清理模型缓存按钮，运维也可通过浏览器站点存储清理历史版本。

## 3. 懒加载、安全和生命周期

1. 父页面必须在用户明确点击后才 lazy-import `ocr/OcrTestPanel`；面板默认导出 `{model: AIModel}`，类型仅从共享 services 导入。
2. 面板打开、上传/拍照选图和裁剪，只解码本机图片。此时不加载 SDK、Worker、ORT WASM 或模型。
3. **点击开始识别**后才动态导入 runtime-client、下载校验模型、加载应用自带 WASM、启动固定应用 Worker。manifest 只能描述两个模型 tar，不能指定 JS、Worker、mjs 或 WASM URL。
4. SDK 在应用自有 Worker 内运行，使用自定义 fetch 回调返回已经校验的模型字节，不走 SDK 官方远程模型默认地址/CDN fallback。
5. SDK 0.4.2 的非内置 Worker ImageData/ImageBitmap 转换路径依赖 `document`，所以将 ImageData 通过已初始化 OpenCV 的 `matFromImageData` 转成 SDK 支持的 `cv.Mat` 输入；不修改第三方包、不伪造推理。
6. 每次任务完成、异常或取消都终止 Worker；取消/卸载还关闭 ImageBitmap、清零 canvas、清空结果。generation + model revision 检查阻止迟到结果覆盖新状态。Worker 阶段超过 120 秒会失败并终止；资源下载可由取消按钮中断。
7. 图片、识别文字、人工确认码不写日志、localStorage、IndexedDB 或 CacheStorage，不访问远程对话/附件接口。确认按钮只保存当前组件内存状态，不发查询请求。模型为公开资源，持久缓存中只有 tar。
8. 文件最多 15 MiB / 2400 万像素 / 单边 16384；解码前检查 JPEG/PNG/WebP 头尺寸，拒绝 SVG、伪装 MIME 和动态 WebP；浏览器校正 EXIF，可手动转 90°，识别前长边缩至 2048。压缩和缩小可能损失细节。
9. 同一 OCR 返回行内仅规范化水平分组空白；不跨行合并，不把 21 位截成 20 位、不补全、不把 O 改成 0。全部编号保持字符串。手动粘贴跨行文本会阻止，避免原生单行 input 自动吞掉换行。
10. 文字框映射回校正/缩放后的整图；选区可拖拽或填写像素。模型分数显示原始尺度小数，**不是正确率/概率**。

### CSP 的已知硬限制（不能省略）

HTTPS/localhost、Worker、WASM/SIMD、Web Crypto、Canvas、createImageBitmap 必须可用。无需开启 COOP/COEP，因为只使用单线程（WASM 文件名带 threaded 不代表实际启用线程）。

**当前上游 OpenCV 4.10 构建在严格禁止 JS 动态求值的 Worker CSP 下不能初始化。** 实测同时给 HTML 与 Worker 响应施加 `script-src 'self' 'wasm-unsafe-eval'` 后，OCR 显示真实 Worker/CSP 错误。只给 HTML 加 CSP 不代表 Worker 受到相同策略，不能据此声称通过。

本实现没有修改/放宽全站 CSP。若生产 Worker 响应已有此限制，不能直接承诺可上线；需要安全评审选择仅对可信 OCR Worker 的受控例外，或另行构建并验证 CSP 兼容 OpenCV。不要把 `unsafe-eval` 无条件添加到全站策略。现有证据 `csp-check.json` 保留这一失败，不将它掩盖为支持。

## 4. 回归与真实浏览器证据

### 自动化

```powershell
npm test -- src/features/ai-models/ocr/__tests__
npm run test:ocr-deployment
npm run typecheck
npm run build
```

OCR 回归共 **33 个测试 / 4 个文件**，涵盖纯逻辑、字符串保零、禁止跨行/修复/截断、manifest 同源路径和资源名单、摘要/大小校验、缓存损坏/不可用、图片头、任务代次、面板不开请求与多行粘贴拦截。测试先出现缺失实现的红灯，再实现并跑绿；未以此宣称全模块 80% 行覆盖率。

实际浏览器 QA（需要现有 Python Pillow、Playwright 和本机 Chrome；这些不是应用运行依赖）：

```powershell
npm run ocr:prepare
npm run dev -- --host localhost --port 3039 --strictPort
# 在另一个终端，从仓库根目录：
python frontend/scripts/ocr-browser-qa.py
```

测试页 `BASE/scripts/ocr-browser-smoke.html` 仅用于 Vite 开发验证，不是正式页面入口，不包含登录信息或真实用户数据；正常应用 build 不把它作为入口。它同样点击后加载面板。页面真实调用同一 OCR 面板和同一模型，没有 OCR 结果 mock。测试脚本故意污染一次模型 HTTP 响应与一次模型缓存，只验证校验拒绝/恢复，不伪造识别输出。

证据输出默认 `.run-logs/model/ocr/`：

- `synthetic-20.png`：合成 Arial 90px 黑白、1500×280 输入，内容 **01234567890123456789**。
- `ocr-desktop.png` / `ocr-mobile.png`：真实框、结果、修正/确认及小数模型分数。
- `network-desktop.json` / `network-mobile.json`：请求 URL、方法、是否含 body，不记录图片或用户业务码。
- `qa-report.json`：具体机器/浏览器时序、正确匹配和生命周期断言。
- `integrity-failure.png`：故意损坏模型摘要时真实拒绝页面。
- `production-evidence/`：通过 Vite 生产打包后的同一烟测入口核验，不只验证开发模式。
- `csp-check.json`：严格 Worker CSP 的真实失败限制。

### 实测边界与结果

浏览器 **Chrome 153.0.8010.52**，Windows 桌面 CPU、localhost；桌面 1280×900 与 390×844 触屏视口模拟。合成清晰 20 位均完整识别；旋转四次回正及手动裁剪后也完整匹配，框坐标正确回映射。**仅一个合成样本，不是准确率统计，更不是实际拍照鲁棒性验收。** 模糊、反光、竖排、透视、低端手机和真实业务码样本仍待验收。

此前生产包基线实测：桌面冷启动 **1.44 秒**（检测 212 ms、识别 42 ms），移动视口模拟冷启动 **1.40 秒**（检测 215 ms、识别 41 ms）。之后重复测试受到共享机器负载影响，开发模式冷启动约 1.8–5.0 秒；具体新一轮值以证据 JSON 为准。缓存避免模型再下载，但每次重新建 Worker/会话，因此暖启动不必然更快。这些 localhost 成绩不能当移动网络下载速度或真机推理 SLA。

已验证：

- 未打开面板、选图未开始时，OCR runtime/模型请求数均为 **0**；生产包也通过。
- 识别与确认过程外域请求 **0**，POST/带 body 请求 **0**，确认动作请求 **0**，控制台编号输出 **0**。
- 热缓存识别不再下载 tar，摘要错误在创建 Worker 前被拒绝，损坏缓存能驱逐并重新校验恢复。
- 每视口测试中 Worker 创建/终止 **4/4**；取消后 ImageBitmap.close 已调用、预览移除且无迟到结果；卸载后再次关闭位图，面板不存在。
- 390px 视口没有横向溢出。但**没有真机、相机授权、Safari 或飞书验证，不声称已支持飞书**。

## 5. 当前不做的事情

不将图片交给服务端，不自动业务查询，不生成校验位规则，不猜测错误数字，不部署未经授权的外域 SDK，不提供额外模型静默降级。官方候选已实际获得并运行，所以本次没有采用候选替代模型。


### 生产集成复审补充：.js MIME 与 clean build

- 新增 5 项部署回归：标准 prebuild 链、配套 .js URL/Worker 类型一致性、固定 hash 缓存无网络复用、离线资源缺失与损坏均 fail closed。
- 无模型/manifest、无生成 runtime 的隔离源码快照，仅复用已安装的 node_modules，直接执行标准 `npm run build` 成功。脚本实际从官方源下载两份模型，最终 dist 包含总计 6,318,080 字节的两份正确 hash tar 和 manifest；ORT 配套文件为 `.js`，没有输出 `.mjs`。证据 `.run-logs/model/ocr/clean-standard-build/{before-build.json,standard-build.log,after-build.json}`。这是 Windows 标准 npm 构建验证，不宣称已执行 Docker 容器构建。
- 新独立生产包与浏览器复测证据保存在 `.run-logs/model/ocr/production-js-mime-evidence/`；同时记录实际请求的配套模块扩展名和响应 Content-Type。真实 Nginx 1.27 静态服务复验由主集成执行，不把 Vite preview 的结果冒充 Nginx 结果。

本次 .js 集成修复后的独立生产包：桌面冷启动 1.90 秒（检测 260 ms / 识别 48 ms），390px 移动视口模拟冷启动 1.70 秒（检测 245 ms / 识别 47 ms）；实际配套模块响应为 text/javascript。未打开/未开始的引擎请求仍为 0，取消、卸载、摘要拒绝及缓存恢复断言继续通过。数据来自 production-js-mime-evidence/qa-report.json，并非真实手机或 Nginx 成绩。
