/**
 * 路由参数解析 helpers（Architecture 2.0 §56）。
 *
 * /schedules/NaN、/schedules/0、/schedules/-3 一律视为非法路由：
 * 不发 /api/schedules/NaN 请求，直接显示 Invalid Route。
 */
export function parsePositiveRouteId(raw: string | undefined): number | null {
  if (!raw) return null;
  const id = Number(raw);
  if (!Number.isInteger(id) || id <= 0) return null;
  return id;
}
