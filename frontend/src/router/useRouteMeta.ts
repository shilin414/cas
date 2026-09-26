import { useMemo } from 'react';
import { useMatches } from 'react-router-dom';
import { readAppRouteMeta, type AppRouteMeta } from './routeMeta';

/**
 * useRouteMeta — 从最深层 matched route 读取 handle.app（Architecture 2.0 §13）。
 *
 * 页面身份（层级 / 父页面 / 权限 / 移动顶栏）全部由路由声明；
 * 组件不得自己解析 pathname 推断这些信息。
 */
export function useRouteMeta(): AppRouteMeta | null {
  const matches = useMatches();
  return useMemo(() => {
    for (let i = matches.length - 1; i >= 0; i -= 1) {
      const meta = readAppRouteMeta(matches[i].handle);
      if (meta) return meta;
    }
    return null;
  }, [matches]);
}
