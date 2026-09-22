# 飞书分享卡片：表格、来源智能体与降级

## 展示契约

- 对话转发、自动化结果投递和业务查询的预览共用 `internal/feishucard`，不使用网页的 React Markdown 渲染器。
- Markdown/GFM 表格转换为飞书 JSON 1.0 原生 `table` 组件，放在卡片根级。单元格使用 `data_type: text`，数值保持原始字符串与列对齐，不转换成浮点数。
- 普通内容仍为纯文本；模型输出中的 `<at>`、HTML 和链接标记不会因此变成可执行卡片指令。
- 原生表格每张卡片最多 5 个；本项目预览每表最多 6 列、8 行，仍受预览字数和高度预算限制。截断会提示打开完整页面，不在原生表格里展示被截断的一半数值。
- 飞书官方文档要求原生表格使用 V7.4 及以上客户端。旧客户端可能显示升级占位；**消息发送成功不代表接收端版本支持渲染**，服务端无法据此自动降级。
- 用户可见品牌使用“小安工作助手”。`/xiaoan-platform/` 路径、包名、配置键、加密派生标识不属于展示文案，不得做全局替换。

## 来源智能体

新分享快照保存智能体名称、默认图标和服务端头像存储引用。卡片中来源智能体与“谁分享了消息”分开展示；转发仍使用分享者／定时任务所有者的用户身份，不改为机器人身份。

自定义头像从受控的 `application-avatars/` 存储读取，经飞书图片上传接口（`image_type=message`）取得 `image_key` 后展示。只读取至多 2 MiB 的受支持图片，不下载任意外部 URL，不把私有存储键暴露给公开分享接口。

头像不存在、旧头像文件被清理、上传超时或缺少图片权限时，使用已保存的默认图标和名称，不能阻断正文转发。历史快照未保存身份时不猜测当时的智能体资料；需要新建分享才能固定当前资料。已发出的飞书卡片不会因代码升级自动改变。

## 双层降级

1. **本地转换／编码失败或新卡片超限**：直接准备旧版纯文本卡片，不先发送已知不可用的 payload。
2. **飞书明确拒绝卡片**：仅当消息接口返回结构化错误 `230099`（卡片创建失败）或 `230025`（消息过大），并且存在不同的旧版 payload 时，最多再发送一次纯文本卡片。

降级保留相同发送用户、目标和完整内容链接，去掉原生表格及自定义图片组件，保留名称／默认图标和“小安工作助手”品牌。业务查询保留原快照与目标对应的幂等 UUID。

**不会触发内容降级的情况**：授权失效、权限不足、限流、网络失败、超时、响应不完整。网络异常时上游可能已经接受消息，不能盲目补发。既有投递队列的重试语义不因此变为 exactly-once。

如果第二次发送也失败，只向调用方返回第二次尝试的错误分类；尤其不能把第一次“明确拒绝”混入错误链，导致第二次“发送状态不确定”被误判为可安全重试。

## 验证与排查

- `feishucard/table_test.go`：原生表格、转义竖线、代码块、列对齐、容量和数字完整性。
- `feishucard/prepare_test.go`：转换失败与旧版 payload。
- `identity/feishu_card_fallback_test.go`：明确拒绝才降级、只尝试一次、超时不补发、第二次错误分类。
- `identity/feishu_card_avatar_test.go`：头像上传、同一用户授权、拒绝任意 URL／越界引用。
- `transport/http/share_agent_test.go`、`sharing/result_test.go`：智能体资料随分享快照固定。
- `transport/http/share_preview_test.go`：模拟飞书消息接口，验证单聊／群聊的原生表格与降级。

以上模拟发送不等同于飞书真实客户端渲染验收。真实发送应由用户向明确批准的测试对象操作，不能为了测试自动转发历史消息或群发。

## 官方参考

- [JSON 1.0 表格组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-components/content-components/table)
- [备注组件中的头像图片](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-components/content-components/note)
- [上传图片](https://open.feishu.cn/document/server-docs/im-v1/image/create)
- [发送消息及错误码](https://open.feishu.cn/document/server-docs/im-v1/message/create)
