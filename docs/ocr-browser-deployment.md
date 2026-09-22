# 浏览器OCR资源与部署

权威资源定义：frontend/src/features/ai-models/ocr/builtin-model.json。当前适配PP-OCRv6 Tiny，资源版本ppocrv6-tiny-20260921。

| 资源 | 字节数 |
| --- | ---: |
| PP-OCRv6_tiny_det | 1792000 |
| PP-OCRv6_tiny_rec | 4526080 |

SHA256、上游提交和下载地址均在该JSON中；不另抄一份可漂移清单。

## 构建

在frontend目录：

```bash
npm ci
npm run ocr:runtime
npm run ocr:prepare
npm run build
```

npm run build的prebuild已包含两项准备。runtime准备WASM/OpenCV运行资产，prepare下载并校验两个固定tar，输出public/ocr-assets/<version>/manifest.json及来源信息，随后被Vite打包。
开发npm run dev只做runtime准备，不自动保证两个模型tar就绪。

## 隔离网络

在有授权网络的构建机运行ocr:prepare，并按定义核对哈希/长度。将完整public/ocr-assets版本目录通过受控渠道预置到构建上下文。
脚本识别已经校验正确的tar后复用；无文件时会访问官方源。不要替换成另一模型、修改SHA绕过失败，或者生产时依赖任意CDN。

## 发布检查

同源URL为/xiaoan-platform/ocr-assets/ppocrv6-tiny-20260921/manifest.json及对应tar。
不存在的资源必须404，不能SPA回退。镜像/压缩包检查真实字节，不仅看HTTP200；模型资产不是1.5MB的假定体积。
路径改变需要重构建manifest、前端和代理，详见[子路径](deployment-subpath.md)。

## 浏览器与安全策略

显式操作OCR时才加载引擎/权重；图片在浏览器本地处理，缓存的是版本化模型不是用户图片。
需要WebAssembly、Worker和足够内存；移动端/目标企业浏览器需实测。当前OpenCV依赖在严格禁止动态求值的CSP下可能受限，不能宣称默认严格CSP已验收，也不能无条件给全站添加unsafe-eval。
生产安全团队应评估具体Worker/运行库策略，在目标CSP下验证。没有验证时保持该限制在发布清单中。
