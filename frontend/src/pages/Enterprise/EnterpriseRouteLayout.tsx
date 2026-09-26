/**
 * EnterpriseRouteLayout — PC/Mobile 企业控制台外壳（Architecture 2.0 §33）。
 *
 * 只负责按设备切换 Presentation；不读取 pathname、不决定页面组件 ——
 * 页面由嵌套子路由（Outlet）渲染，权限由 RoutePermissionBoundary 判定。
 */
import { useIsMobile } from '@/shell/useIsMobile';
import DesktopEnterpriseLayout from './desktop/DesktopEnterpriseLayout';
import MobileEnterpriseLayout from './mobile/MobileEnterpriseLayout';

export default function EnterpriseRouteLayout() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileEnterpriseLayout /> : <DesktopEnterpriseLayout />;
}
