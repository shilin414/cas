import { useCallback, useMemo } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useIsMobile } from '@/shell/useIsMobile';
import { useRouteMeta } from './useRouteMeta';

/**
 * appNavigation history state（Architecture 2.0 §19）。
 *
 * 飞书 WebView / Safari / OAuth 跳转都会污染 window.history.length，
 * 所以判断「这条 entry 是否由本应用 PUSH」只能依赖我们自己写入的 state，
 * 绝不允许用 history.length 猜。
 */
export interface AppNavigationState {
  appNavigation?: {
    type: 'root' | 'push';
    from?: string;
  };
}

export interface AppNavigation {
  /** 一级导航切换：Mobile replace（不堆返回栈），PC 保持 push 体验。 */
  switchRoot(path: string): void;
  /** 详情页进入：始终 PUSH，并记录来源供 back() POP。 */
  pushPage(path: string): void;
  /** 原地替换（如保存成功后 new → /schedules/:id）。 */
  replacePage(path: string): void;
  /** 返回：应用内 PUSH 过的 entry 用 POP；Direct Link 用 parent/root replace。 */
  back(): void;
}

/** 把 parent 模板里的 :param 用当前路由参数填充（审查 M-1）。 */
function fillRouteParams(template: string, params: Record<string, string | undefined>): string {
  return template.replace(/:([A-Za-z0-9_]+)/g, (match, name: string) => params[name] ?? match);
}

export function useAppNavigation(): AppNavigation {
  const navigate = useNavigate();
  const location = useLocation();
  const params = useParams();
  const isMobile = useIsMobile();
  const routeMeta = useRouteMeta();

  const currentHref = useMemo(
    () => location.pathname + location.search + location.hash,
    [location.pathname, location.search, location.hash],
  );

  const switchRoot = useCallback((path: string) => {
    if (isMobile) {
      navigate(path, {
        replace: true,
        state: { appNavigation: { type: 'root' } },
      });
      return;
    }
    // PC 保持现有体验：正常 push。
    navigate(path);
  }, [isMobile, navigate]);

  const pushPage = useCallback((path: string) => {
    navigate(path, {
      state: {
        appNavigation: {
          type: 'push',
          from: currentHref,
        },
      },
    });
  }, [currentHref, navigate]);

  const replacePage = useCallback((path: string) => {
    navigate(path, { replace: true });
  }, [navigate]);

  const back = useCallback(() => {
    const state = (location.state ?? null) as AppNavigationState | null;
    if (state?.appNavigation?.type === 'push') {
      // 这条 entry 是本应用 PUSH 的：POP 回来源，iOS 侧滑语义一致。
      navigate(-1);
      return;
    }
    // Direct Link / 刷新进入：没有应用内来源，回逻辑父页面。
    // parent 可能带路由参数模板（如 '/schedules/:scheduleId'）——用当前
    // 路由的 params 填充，避免 replace 到字面量 ':scheduleId'。
    const parent = routeMeta?.parent;
    if (parent) {
      navigate(fillRouteParams(parent, params), { replace: true });
      return;
    }
    navigate(routeMeta?.root ?? '/', { replace: true });
  }, [location.state, navigate, params, routeMeta]);

  return useMemo(
    () => ({ switchRoot, pushPage, replacePage, back }),
    [switchRoot, pushPage, replacePage, back],
  );
}
