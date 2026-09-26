/**
 * ScheduleDetailRoute — /schedules/:scheduleId（Architecture 2.0 §56–§57）。
 *
 * Mobile：MobileScheduleDetailPage（全屏页面）。
 * Desktop：列表背景 + ScheduleDetailDrawer（视觉保持 Drawer，但开关由
 * 路由决定：关闭 = navigation.back，不是 setOpen(false)）。
 */
import { Result } from 'antd';
import { useParams } from 'react-router-dom';
import { useIsMobile } from '@/shell/useIsMobile';
import { useAppNavigation } from '@/router/useAppNavigation';
import { parsePositiveRouteId } from '@/router/routeParams';
import { MobileScheduleDetailPage } from '@/components/Schedules/MobileScheduleDetailPage';
import { ScheduleDetailDrawer } from '@/components/Schedules/ScheduleDetailDrawer';
import { DesktopScheduleCenter } from './DesktopScheduleCenter';

export default function ScheduleDetailRoute() {
  const { scheduleId } = useParams();
  const isMobile = useIsMobile();
  const navigation = useAppNavigation();
  const id = parsePositiveRouteId(scheduleId);

  // 非法路由参数（NaN / 0 / 负数 / 字符串）不发 /api/schedules/NaN。
  if (id === null) {
    return (
      <Result
        status="404"
        title="任务不存在"
        subTitle="任务地址无效。"
      />
    );
  }

  if (isMobile) {
    return <MobileScheduleDetailPage scheduleId={id} />;
  }

  // Desktop：列表 + Drawer。Drawer 的关闭 = 返回（POP / parent fallback）。
  return (
    <>
      <DesktopScheduleCenter />
      <ScheduleDetailDrawer
        open
        scheduleId={id}
        onClose={() => navigation.back()}
      />
    </>
  );
}
