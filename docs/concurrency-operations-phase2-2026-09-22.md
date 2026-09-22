# 第二批交付：管理员运行中心与角色可观测性

日期：2026-09-22。保留第一批未提交改动，本批也未提交、推送或部署。

## 已交付

1. 管理员运行中心 `/enterprise/operations`：桌面/手机容量卡片、状态告警、Provider额度、任务元数据筛选与键集分页。
2. 后台管理API：复用 `run.monitor.read`/`provider.manage`，保留Session/CSRF；额度修改带原值冲突检查、必填原因和同事务审计。
3. 容量一致观测：按run去重，单SQL输出受控/无槽外部/有效占用，并使用同一个数据库采样时间，避免旧快照与漂移时钟引发假失控告警。
4. Worker/Delivery/Scheduler私有指标和健康端点：默认关闭，显式开启；真实循环成功与依赖/采样新鲜度决定Ready，不以后台timer伪造业务健康。
5. 监控资源隔离：独立Redis探针连接与短超时、不使用业务池；按真实循环周期验证健康窗口；采样至少5秒且不小于两倍超时，完成后等待、失败退避。
6. 防误操作/假成功：权限加载与切账号隔离；手机直达主动解析权限；409刷新后重新确认；异常200响应不算额度修改成功；读取失败不回退到零。
7. 查询索引源码0045及手工发布注意事项，没有执行迁移。

没有加入任务取消、重试、补发、批量清队列等副作用操作，也没有自动发送告警到飞书。角色探针不覆盖Relay、DirectoryScheduler或全部在途handler，不是全系统及上游健康证明。

## 独立审查与闭环

- 安全审查：容量统计跨越旧槽过期时点造成假告警，已修复为固定DB采样时间和单SQL去重聚合；复核关闭。
- TypeScript审查：移动端staff直达缺少权限初始化，已补显式加载/错误/重试；类型夹具和成功响应验证也已复核。
- Go监控审查：探针共享业务连接、delivery健康窗口不足、无最小采样周期三项已补红绿回归并修复；最终独立复核关闭。
- 观测链发现逐条错误被吞时，保留已完成事务和计数，同时向健康检查返回聚合错误，避免部分失败报成功。

## 最终验证

| 项目 | 结果 |
| --- | --- |
| Go全量构建 / vet | PASS |
| 后端离线测试 | 30个包直接PASS；另2个包因Windows临时目录执行权限，在固定目录重新编译执行后PASS；17个包无测试文件 |
| 前端TypeScript / feature ESLint | PASS |
| 前端全量测试 | 1162/1162 PASS（最终限制测试worker并行度） |
| Vite bundle | PASS；隔离输出目录，未运行OCR下载/生产部署步骤；仍有既有大chunk提示 |
| 浏览器 | 真实headless Edge运行组件，8条mock API旅程PASS；桌面/手机截图已查看 |
| 新后端包语句覆盖率 | operations 81.6%；capacityview 100%；rolemonitor 98.1% |
| SQL生成与索引配对 | sqlc重生成无额外变化；0045 up/down索引名称配对；未执行DDL |
| 容量SQL谓词 | 7项SQLite集合/时间模型断言通过；不是MySQL事务/优化器证明 |

全量前端测试曾有一条未改动的既有定时表单用例在约5秒默认时限超时；单独复跑通过，降低测试并行后1162条全量通过。没有修改或跳过该业务用例。浏览器首次验证修正了测试选择器及既有外部字体的离线处理；最终没有未预期网络请求或页面异常。

浏览器API全部由本地假数据响应，原有Google字体请求被本地空CSS替代，没有访问真实后端或第三方字体服务。它证明交互/布局/请求边界，不证明真实数据库或真实上游并发。

## 上线前尚需完成

- 隔离MySQL5.7/Redis下多进程并发、负载与故障验证；0045真实DDL up/down、执行计划与大表资源预算。
- `promtool`配置和规则加载验证、目标网络隔离、抓取与通知链联调。
- race检测（本机CGO关闭且无gcc），以及可用环境中的静态安全工具检查。
- 确认所有相关角色版本一致、已有Provider额度有效，再由授权人员设置监控地址。不可因为页面能显示就跳过生产验收。

这些条件没有被mock、单元测试或跳过的集成用例替代。本轮没有读取生产容量、触发真实任务或发送飞书消息。

## 入口

- 使用与API说明：`docs/operations-center.md`
- 监控配置：`deploy/monitoring/README.md`
- 监控复审：`deploy/monitoring/P2_REVIEW.md`
- 本地验证汇总：verification-summary.json（本机文件 `.planning/concurrency-operations-20260922/verification-summary.json`，未入库）
- 桌面预览（模拟数据）（本机文件 `.planning/concurrency-operations-20260922/operations-desktop.png`，未入库）
- 手机预览（模拟数据）（本机文件 `.planning/concurrency-operations-20260922/operations-mobile.png`，未入库）
- 额度确认预览（模拟数据）（本机文件 `.planning/concurrency-operations-20260922/operations-confirmation.png`，未入库）
