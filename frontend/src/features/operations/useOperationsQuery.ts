import { useEffect, useState, useSyncExternalStore } from 'react';
import { captureSessionGeneration, registerSessionReset, sessionStillCurrent } from '@/stores/resetSessionState';

const listeners = new Set<() => void>();
registerSessionReset(() => { for (const listener of listeners) listener(); });
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };

// Also clear already-rendered local state on reset, even if the next account has the same user ID.
export function useOperationsSession() {
  return useSyncExternalStore(subscribe, captureSessionGeneration, captureSessionGeneration);
}

export function useOperationsQuery<T>(load: (signal: AbortSignal) => Promise<T>, revision: number) {
  const [state, setState] = useState<{ loading: boolean; failed: boolean; data: T | null }>({ loading: true, failed: false, data: null });
  useEffect(() => {
    const controller = new AbortController();
    const generation = captureSessionGeneration();
    const current = () => !controller.signal.aborted && sessionStillCurrent(generation);
    // Never leave an old snapshot visible as if it belonged to the new query.
    setState({ loading: true, failed: false, data: null });
    load(controller.signal).then(data => {
      if (current()) setState({ loading: false, failed: false, data });
    }).catch(() => {
      if (current()) setState({ loading: false, failed: true, data: null });
    });
    return () => controller.abort();
  }, [load, revision]);
  return state;
}
