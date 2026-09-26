/**
 * ScheduleListRoute — /schedules 列表页（Architecture 2.0 §51–§53）。
 *
 * 只做 PC/Mobile Presentation 切换；列表内部交互（筛选/搜索/编辑器）
 * 由各 Center 组件持有。编辑器/详情不再是本地 state —— 它们有真实 URL。
 */
import { useIsMobile } from '@/shell/useIsMobile';
import { DesktopScheduleCenter } from '@/pages/Schedules/DesktopScheduleCenter';
import { MobileScheduleCenter } from '@/pages/Schedules/MobileScheduleCenter';

export default function ScheduleListRoute() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileScheduleCenter /> : <DesktopScheduleCenter />;
}
