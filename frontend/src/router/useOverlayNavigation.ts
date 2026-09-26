import { useCallback, useRef } from 'react';

/**
 * useOverlayNavigation — Overlay 内的导航时序（Architecture 2.0 §68）。
 *
 * requestNavigation(target)：记录目标并关闭 Overlay；
 * afterOpenChange(false)：动画真正结束后才执行导航。
 * 绝不用 setTimeout 猜动画时长。
 */
export function useOverlayNavigation(navigate: (target: string) => void) {
  const pendingRef = useRef<string | null>(null);
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;

  /** 点击项时调用：先记目标再关 Overlay；导航延迟到动画结束。 */
  const requestNavigation = useCallback((target: string) => {
    pendingRef.current = target;
  }, []);

  /** 传给 AntD Drawer/Modal 的 afterOpenChange。 */
  const afterOpenChange = useCallback((open: boolean) => {
    if (open) return;
    const target = pendingRef.current;
    if (target === null) return;
    pendingRef.current = null;
    navigateRef.current(target);
  }, []);

  return { requestNavigation, afterOpenChange };
}
