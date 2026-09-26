/**
 * routeMeta — 页面身份与导航层级的单一声明（Architecture 2.0 §11）。
 *
 * Router 声明 URL，routeMeta 声明「这个页面是谁、父页面是谁、权限是什么、
 * 移动端顶栏长什么样」。业务页面不再自行解析 pathname 决定层级。
 */
import type { PermissionRule } from './permissions';

export type RouteLevel =
  | 'root'
  | 'detail'
  | 'fullscreen';

export type MobileRouteMode =
  | 'workspace'
  | 'page'
  | 'detail'
  | 'console';

export type MobileRouteAction =
  | 'none'
  | 'new-task'
  | 'create'
  | 'search'
  | 'more';

export interface AppRouteMeta {
  /** 全局唯一页面 id（如 'home'、'enterprise-resource-agents'）。 */
  id: string;

  /** 页面层级：root 一级导航；detail 从 parent PUSH 进入；fullscreen 独占视口。 */
  level: RouteLevel;

  /** 所属一级根路径（detail 页面的返回锚点）。 */
  root: string;

  /** 逻辑父页面路径；Direct Link 返回时的 replace 目标。 */
  parent?: string;

  /** 路由进入权限；缺省 = 不要求企业权限。 */
  permission?: PermissionRule;

  shell?: {
    padded?: boolean;
    hideHeader?: boolean;
    hideSidebar?: boolean;
  };

  mobile?: {
    mode: MobileRouteMode;
    title?: string;
    action?: MobileRouteAction;
    showAgentSwitcher?: boolean;
  };
}

/** 从 route handle 中取 AppRouteMeta 的类型收窄助手。 */
export function readAppRouteMeta(handle: unknown): AppRouteMeta | null {
  if (handle && typeof handle === 'object' && 'app' in handle) {
    const app = (handle as { app: unknown }).app;
    if (app && typeof app === 'object' && 'id' in app && 'level' in app && 'root' in app) {
      return app as AppRouteMeta;
    }
  }
  return null;
}
