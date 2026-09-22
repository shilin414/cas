# Aily 执行过程与最终回复：已知边界和排查指南

## 先区分事实与风险

> **目前已核查的样本中，没有确认出现“执行过程与最终答案混在同一个、没有分离标记的文本块里”的情况。**
> 这是尚未证实的上游风险边界，不是已发现但未修复的缺陷；未做全量历史排查，也不能保证未来不会出现。

| 情况 | 当前证据与结论 |
| --- | --- |
| 文本块没有明确的“过程／答案”标签 | 已观察到。案例中的每个文本项只有 `type: "text"` 和 `text`，没有独立的过程／答案分类字段。 |
| 多个文本块被平台拼成一条混合回复 | 已确认并修复。旧后端把所有文本项拼接成最终答案。 |
| 单个文本块内部同时包含过程与答案，且没有分离标记 | 目前没有确证。遇到新的混杂现象，应重点检查，但不能仅凭截图就认定上游如此返回。 |

### 已验证案例

会话 `41511` 的 Aily **终态查询结果**中，`content` 包含 7 个独立文本项：前 6 项为执行过程，第 7 项为最终回复。页面当时显示混杂，是因为旧版 `ExtractFinalText` 把这些项全部拼接，**并非已经证明上游在同一项里混写了过程和答案**。

以下仅为脱敏结构示例，文字不代表实际业务数据：

```json
{
  "status": "Completed",
  "content": [
    { "type": "text", "text": "正在检查权限。" },
    { "type": "text", "text": "正在查询数据。" },
    { "type": "text", "text": "查询完成。\n\n这里是最终结果。" }
  ]
}
```

**“没有分类标签”不等于“没有文本块边界”。** 本文所说的文本块，是终态结果 `content[]` 中的独立文本项，不是 HTTP 数据包、SSE 帧或 `content.delta` 增量片段。增量片段的数量不能用来判断最终答案的语义边界。

## 当前实现及限制

- 对成功结果，`Mapper.SplitResponseText` 取最后一个非空白 `type: "text"` 项作为最终回复，将此前有效文本项用空行连接为执行过程；不是固定取“前 6 项／第 7 项”。附件项不会成为最终文字回复。
- 对失败／取消结果，当前映射将有效文字保留为过程，不提升为成功答案。
- 只有一个有效文本项时，保留该项全文。**不会根据“我先”“正在查询”“查询完成”等词句猜测并删掉一部分。** 如果上游确实在这一项内混写且无明确标记，本地就无法可靠分离。
- “最后一个有效文本项是最终回复”来自已观察到的 Aily 返回结构，**不是已核实的官方通用保证**。如果后续出现最后一项仅为补充说明、答案跨多个文本项等反例，也需要重新核对上游语义，而不是继续假设规则必然成立。
- 分离后的 `text` / `process_text` 进入 Run 输出和终态事件；有最终文字的持久化助手消息同时保存 `metadata.process_text`，前端分别恢复过程和答案。
- 流式阶段的过程是暂时内容；终态中的 `process_text` 会对其校正，包括用空字符串清除重复的最终答案。终态字段优先于较旧的 GET 补查结果。

## 再遇到混杂时，按这个顺序排查

1. **定位同一次执行。** 记录 Conversation ID、Run ID、上游 chat ID、环境和 Worker 实际构建版本。区分历史消息与修复后新执行：历史正文不会因代码升级自动改写。
2. **只读取得原始终态结果。** 使用该次执行所属身份的授权，查询已有 Aily chat；不要为了取证重新发起任务。保留原始 `content[]` 顺序、每项类型／字段和原始换行，不能先拼接文字再分析。
3. **逐项确认边界。** 对比每一项的内容：
   - 过程和答案分属不同项，但最终正文仍包含过程：优先检查 Mapper、Worker 是否加载新构建，以及结果持久化链路。
   - 只有某一个文本项自身就同时包含过程和答案：才有证据确认“单块混写”；记录准确项索引与脱敏复现数据。
   - 最终答案分散在多个项，或最后一项并非完整回复：属于当前“最后一项”约定的反例，应重新审查提取规则。
4. **逐层比较同一个 Run。** 顺序查看：上游终态 `content[]` → Mapper 的 `text` / `process_text` → `runs.output` → `run.completed` payload → 历史 `messages.content` / `messages.metadata.process_text` → 前端消息状态。
5. **根据出现时机定位。**
   - 首次结束时正确，随后过程又出现：检查 GET 补查是否覆盖了权威终态，尤其是 `process_text: ""` 是否被错误地当作“字段缺失”。
   - 当前页面正确，刷新后混杂或过程丢失：检查历史正文／元数据和保存时的错误日志，不要只调整前端折叠样式。
   - 数据分离正确，仅表格／图片显示异常：转查 Markdown/GFM 或附件引用渲染，不要改动答案提取规则。
6. **先固化回归，再改规则。** 新反例应保存为脱敏测试用例，包含实际文本项边界，并同时断言完整答案没有被截断、过程没有被误放到最终正文。

## 确认单块混写后的处理原则

优先寻找上游明确的输出字段、消息类型或可靠分隔契约，并核实其适用范围。没有可靠依据时，应明确说明不能可靠分离，保留原文供授权排查；不能用关键词删除或固定截取最后几句来“隐藏问题”。上游配置或提示词调整需要单独验证，不能把其效果当作接口保证。

原始抓取可能包含内部业务数据、权限信息和链接：仅存放在受控诊断位置，不将 token、密钥或未经脱敏的回复提交到 Git。修复代码与修复历史数据是两件事，不能顺手批量改写已有消息或终态事件。

## 代码与回归入口

| 层级 | 入口 |
| --- | --- |
| 上游结果提取 | [mapper.go](../backend-go/internal/integrations/aily/mapper.go)：`SplitResponseText`、`ExtractFinalText` |
| Worker 汇总与历史元数据 | [executor.go](../backend-go/internal/integrations/aily/executor.go)：`finalizeFromResult`、`assistantMetadata` |
| 持久化与终态事件 | [finalize.go](../backend-go/internal/execution/finalize.go)：`FinalizeOwnedRun`、`finishEventPayload` |
| 前端事件／补查／历史恢复 | [useRunChatStore.ts](../frontend/src/stores/useRunChatStore.ts)：`applyEvent`、`finalizeRun`、`loadConversation` |
| 执行过程展示 | [AssistantResponse.tsx](../frontend/src/components/Chat/AssistantResponse.tsx) |
| 提取规则回归 | [mapper_test.go](../backend-go/internal/integrations/aily/mapper_test.go)：`TestExtractFinalTextDoesNotConcatenateExecutionProcess`、`TestSplitResponseTextPreservesBoundaries` |
| 前端校正和补查回归 | [useRunChatStore.test.ts](../frontend/src/stores/__tests__/useRunChatStore.test.ts)、[useRunChatStore.races.test.ts](../frontend/src/stores/__tests__/useRunChatStore.races.test.ts) |

维护要求：一旦捕获“单块混写”或其他消息边界反例，应同步更新本文的证据状态、当前策略与对应回归测试，不能继续保留“未发现”的旧结论。
