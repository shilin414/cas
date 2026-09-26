# Navigation Overlay 审计（Architecture 2.0 Commit 19）

> 生成日期：2026-09-26 · 基线：`refactor/navigation-authz-v2`
>
> 判定标准（§66）——满足两项以上即为 Page：
> 独立 Header / 独立返回 / 可继续进入下一层 / 刷新后有恢复价值 /
> Deep Link 有意义 / 占满主要 viewport。
> Picker 即便 Full Screen，若不是独立业务页面，也不 Route 化。

## 结论总览

| 类别 | 数量 | 处置 |
|---|---|---|
| 已 Route 化 | 4 | 本轮完成 |
| 保持 Overlay（正确） | 33 | 不动 |
| 后续可考虑 Route 化 | 4 | 低优先级，暂不动 |

## 已 Route 化（本轮 Commit 15–18 完成）

| Component | 原用途 | 判定 | 目标 URL |
|---|---|---|---|
| MobileScheduleDetail（已删） | 移动自动化详情全屏抽屉 | Page：独立返回+可进编辑+deep link 有意义 | `/schedules/:scheduleId` |
| MobileScheduleEditor（已删） | 移动自动化编辑全屏抽屉 | Page：保存后要 replace 到详情（§62） | `/schedules/:scheduleId/edit`、`/schedules/new` |
| ScheduleDetailDrawer（Desktop） | 桌面详情抽屉 | 视觉保持 Drawer，开关由路由驱动 | `/schedules/:scheduleId` |
| ScheduleEditorModal（Desktop） | 桌面编辑弹窗 | 视觉保持 Modal，开关由路由驱动 | `/schedules/:scheduleId/edit`、`/schedules/new` |

## 保持 Overlay（正确分类，禁止无脑 Route 化）

| Component | 当前用途 | 判定 | 理由 |
|---|---|---|---|
| MobileActionSheet | ••• 二级动作面板 | Overlay | Action 集合 |
| MobileFullScreenDrawer | 容器组件 | Overlay 容器 | 供真正 Overlay 使用 |
| MobileAttachmentSheet | 聊天附件选择 | Overlay | Picker |
| MobileCatalogSheet | 目录浏览 | Overlay | Picker/浏览 |
| MobileSkillSheet | 技能选择 | Overlay | Picker |
| MobileAccessGroupPicker | 企业用户组选择 | Overlay | Picker（多选回填） |
| MobileDepartmentPicker | 部门选择 | Overlay | Picker（树选择回填） |
| MobileUserPicker | 人员选择 | Overlay | Picker（远程搜索回填） |
| MobilePermissionEditor | 授权编辑 | Overlay | 页面内编辑层，保存回列表 |
| AgentEditorModal | 智能体编辑 | Overlay | 表单弹窗 |
| AgentAvatarModal | 头像编辑 | Overlay | 表单弹窗 |
| ChatApplicationView 内弹窗 | 应用配置 | Overlay | 局部操作 |
| FeishuForwardModal | 消息转发 | Overlay | Action |
| RunChatPanel 内弹窗 | 聊天操作 | Overlay | Action |
| NavigationSettingsModal | 导航偏好设置 | Overlay | Quick Settings |
| FolderPickerModal | 文件夹选择 | Overlay | Picker |
| CapabilityPickerDialog/Sheet | 能力选择 | Overlay | Picker |
| AIModelsPage/Editors/RemoteTestPanel 弹窗 | 模型配置/测试 | Overlay | 表单/操作 |
| CapacityDialog | 容量查看 | Overlay | Action |
| AgentLifecyclePanel / OrganizationGovernancePanel 弹窗 | 企业操作 | Overlay | 操作确认 |
| AccessPage / ResourcePage / MobileAuditPage / MobileResourcePage / AccessGroupsPage / AdminsPage 内弹窗 | 企业各页编辑 | Overlay | 页面内 CRUD 表单 |
| SkillsPage / WorkflowsPage 弹窗 | 技能/工作流操作 | Overlay | 操作 |
| MobileAppShell Drawer | 主导航抽屉 | Overlay | Root Switch 已用 replace（Commit 05） |
| ScheduleBoundaryFields 内弹窗 | 时间选择辅助 | Overlay | Picker |

## 后续可考虑 Route 化（低优先级）

| Component | 判定依据 | 备注 |
|---|---|---|
| BusinessApp 内视图切换 | 有独立业务视图 | 无 deep link 需求，暂缓 |
| ChatApplicationView 编辑态 | 占满视口 | 已有独立编辑路由，内部弹窗属局部 |
| OrganizationGovernancePanel 大型面板 | 内容较重 | 等 Enterprise 后续迭代 |
| AgentLifecyclePanel 大型面板 | 内容较重 | 同上 |

## 已废弃组件

- `MobileScheduleDetail.tsx`：被 `MobileScheduleDetailPage.tsx`（路由版）取代并删除。
- `DesktopEnterpriseConsole.tsx` / `MobileEnterpriseConsole.tsx` / `EnterprisePage.tsx`：Outlet 迁移后删除。
- `SchedulesPage.tsx`：列表由 `ScheduleListRoute` 直接挂载，兼容壳已删（CSS 保留共享）。
- `MobileScheduleEditor.tsx`：生产路由已由 `ScheduleEditorRoute` 接管；文件保留仅为
  `automationEditorUI.test.tsx` 的编辑器交互回归载体（测的是 ScheduleEditorFields +
  useScheduleEditor 状态机，非导航）。
