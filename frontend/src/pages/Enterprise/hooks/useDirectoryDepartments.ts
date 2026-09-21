import { useCallback, useEffect, useRef, useState } from 'react';
import { enterpriseApi, type DirectoryDepartment } from '../enterpriseApi';

export interface UseDirectoryDepartmentsOptions {
  query?: string;
  enabled?: boolean;
  limit?: number;
  debounceMs?: number;
  includeInactive?: boolean;
  sessionKey?: string | number | null;
}

const dedupeById = (items: DirectoryDepartment[]) => {
  const seen = new Set<number>();
  const out: DirectoryDepartment[] = [];
  for (const item of items) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    out.push(item);
  }
  return out;
};

export function useDirectoryDepartments({
  query = '', enabled = true, limit = 50, debounceMs = 300,
  includeInactive = false, sessionKey,
}: UseDirectoryDepartmentsOptions) {
  const [items, setItems] = useState<DirectoryDepartment[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(enabled);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [debouncedQuery, setDebouncedQuery] = useState(query);
  const requestIdRef = useRef(0);
  const loadMoreSeqRef = useRef(0);
  const prevSessionRef = useRef(sessionKey);

  if (prevSessionRef.current !== sessionKey) {
    prevSessionRef.current = sessionKey;
    requestIdRef.current += 1;
    loadMoreSeqRef.current += 1;
    setItems([]); setCursor(null); setHasMore(false); setError(null);
    setLoadingMore(false); setDebouncedQuery(query); setLoading(enabled);
  }

  useEffect(() => {
    if (!enabled) { setDebouncedQuery(query); return undefined; }
    if (query === debouncedQuery) return undefined;
    const timer = setTimeout(() => setDebouncedQuery(query), debounceMs);
    return () => clearTimeout(timer);
  }, [enabled, query, debouncedQuery, debounceMs]);

  useEffect(() => {
    if (enabled) return;
    requestIdRef.current += 1;
    loadMoreSeqRef.current += 1;
    setLoading(false); setLoadingMore(false);
  }, [enabled]);

  const fetchFirstPage = useCallback(async () => {
    if (!enabled) return;
    const requestId = ++requestIdRef.current;
    loadMoreSeqRef.current += 1;
    setLoadingMore(false); setLoading(true); setError(null);
    try {
      const page = await enterpriseApi.departmentPage({
        q: debouncedQuery.trim() || undefined,
        ...(includeInactive ? { include_inactive: true } : {}), limit,
      });
      if (requestIdRef.current !== requestId) return;
      setItems(dedupeById(page.results));
      setCursor(page.next_cursor ?? null);
      setHasMore(Boolean(page.next_cursor));
    } catch {
      if (requestIdRef.current !== requestId) return;
      setError('加载部门失败'); setCursor(null); setHasMore(false);
    } finally {
      if (requestIdRef.current === requestId) setLoading(false);
    }
  }, [enabled, debouncedQuery, includeInactive, limit]);

  useEffect(() => { void fetchFirstPage(); }, [fetchFirstPage, sessionKey]);

  const loadMore = useCallback(async () => {
    if (!enabled || !cursor || loading || loadingMore) return;
    const requestId = requestIdRef.current;
    const seq = ++loadMoreSeqRef.current;
    setLoadingMore(true);
    try {
      const page = await enterpriseApi.departmentPage({
        q: debouncedQuery.trim() || undefined,
        ...(includeInactive ? { include_inactive: true } : {}), limit, cursor,
      });
      if (requestIdRef.current !== requestId) return;
      setItems((current) => dedupeById([...current, ...page.results]));
      setCursor(page.next_cursor ?? null); setHasMore(Boolean(page.next_cursor)); setError(null);
    } catch {
      if (requestIdRef.current === requestId) setError('加载更多失败');
    } finally {
      if (loadMoreSeqRef.current === seq) setLoadingMore(false);
    }
  }, [enabled, cursor, loading, loadingMore, debouncedQuery, includeInactive, limit]);

  return { items, hasMore, loading, loadingMore, error, loadMore, refresh: fetchFirstPage };
}
