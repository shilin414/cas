import { useEffect } from 'react';
import { useLocation, useNavigationType } from 'react-router-dom';
import { useRouteMeta } from './useRouteMeta';

/**
 * useRouteScrollRestoration — 会话内滚动位置恢复（Architecture 2.0 §78–§80）。
 *
 *   离开页面   → 保存 scrollTop（按 location.key）
 *   PUSH       → 默认回到顶部
 *   POP        → 恢复 location.key 对应位置
 *   Root 切换  → 每个一级 root 独立记忆最近位置，切回来恢复
 *
 * 不持久化（无 localStorage / persist）：刷新清掉没关系。
 */
const scrollByLocationKey = new Map<string, number>();
/** 一级 root 最近滚动位置（root path → scrollTop）。 */
const scrollByRoot = new Map<string, number>();

function findScrollContainer(): HTMLElement | null {
  // 优先项目滚动容器，回退 document 滚动。
  const container = document.querySelector<HTMLElement>(
    '.mobile-shell__main, .chat-messages, [data-scroll-container]',
  );
  if (container) return container;
  return document.scrollingElement as HTMLElement | null;
}

function readScrollTop(): number {
  const container = findScrollContainer();
  return container ? container.scrollTop : window.scrollY;
}

function writeScrollTop(top: number) {
  const container = findScrollContainer();
  if (container) container.scrollTop = top;
  else window.scrollTo({ top });
}

/** 测试钩子：重置会话内记忆。 */
export function __resetScrollMemoryForTests() {
  scrollByLocationKey.clear();
  scrollByRoot.clear();
}

export function useRouteScrollRestoration() {
  const location = useLocation();
  const navigationType = useNavigationType();
  const routeMeta = useRouteMeta();

  useEffect(() => {
    const wasPop = navigationType === 'POP';

    if (wasPop) {
      const saved = scrollByLocationKey.get(location.key);
      // POP 恢复：等待新页面布局稳定后写回。
      const restore = () => writeScrollTop(saved ?? scrollByRoot.get(routeMeta?.root ?? '') ?? 0);
      restore();
      // 内容懒加载可能改变高度：再补一次（帧级，不额外计时器语义）。
      requestAnimationFrame(restore);
    } else if (routeMeta?.level === 'root') {
      // Root 切换（replace）恢复该 root 上次位置。
      const saved = scrollByRoot.get(routeMeta.root);
      if (saved != null) {
        const restore = () => writeScrollTop(saved);
        restore();
        requestAnimationFrame(restore);
      }
    } else {
      // PUSH 进详情：回到顶部。
      writeScrollTop(0);
    }

    // 滚动期间持续记录（而不是只在 cleanup 读取——PUSH 写 0 会覆盖）：
    // location.key / root 双记账，离开后 POP 能精确恢复。
    const onScroll = () => {
      const top = readScrollTop();
      scrollByLocationKey.set(location.key, top);
      if (routeMeta?.root) scrollByRoot.set(routeMeta.root, top);
    };
    const container = findScrollContainer();
    container?.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('scroll', onScroll, { passive: true });

    // 首帧也记一次初始位置（PUSH→0 / 恢复值）。
    onScroll();

    return () => {
      container?.removeEventListener('scroll', onScroll);
      window.removeEventListener('scroll', onScroll);
      // 会话内存上限：避免长会话 Map 无限增长。
      if (scrollByLocationKey.size > 200) {
        const firstKey = scrollByLocationKey.keys().next().value;
        if (firstKey !== undefined) scrollByLocationKey.delete(firstKey);
      }
    };
  }, [location.key, navigationType, routeMeta?.level, routeMeta?.root]);
}
