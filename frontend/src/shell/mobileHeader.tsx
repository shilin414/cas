import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import type { MobileHeaderMode } from './useShellChrome';

export type MobileHeaderAction = 'create' | 'search' | 'more' | 'none';

/**
 * 动态 Header 层唯一允许的形状（Architecture 2.0 §27/§86–§88）：
 * 静态部分（mode/title/showBack）全部由 Route Meta 决定，动态层只覆盖 action。
 */
export interface MobileHeaderOverride {
  action?: MobileHeaderAction;
  onAction?: () => void;
}

type LayerId = symbol;

interface MobileHeaderContextValue {
  /** Registration-ordered layers; later layers win field conflicts. */
  layers: MobileHeaderOverride[];
  setLayer: (id: LayerId, override: MobileHeaderOverride | null) => void;
}

const MobileHeaderContext = createContext<MobileHeaderContextValue | null>(null);

const EMPTY_LAYERS: MobileHeaderOverride[] = [];

function overrideEqual(
  a: MobileHeaderOverride | null,
  b: MobileHeaderOverride | null,
): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (Object.keys(a) as Array<keyof MobileHeaderOverride>).every(
    (key) => a[key] === b[key],
  ) && Object.keys(a).length === Object.keys(b).length;
}

/**
 * LAYERS, not a single slot: the enterprise console nests overrides — the
 * console shell sets the sub-page title (← 智能体管理) while the resource
 * page inside it binds the trailing ＋ action. A single slot made them
 * clobber each other (child effect ran first, parent overwrote it), which
 * silently deleted the create button. Each `useMobileHeader` caller owns
 * exactly its own layer and only the fields it defines.
 */
export function MobileHeaderProvider({ children }: { children: React.ReactNode }) {
  const [layers, setLayers] = useState<Array<{ id: LayerId; override: MobileHeaderOverride }>>([]);

  const setLayer = useCallback((id: LayerId, override: MobileHeaderOverride | null) => {
    setLayers((prev) => {
      if (!override) {
        const next = prev.filter((layer) => layer.id !== id);
        return next.length === prev.length ? prev : next;
      }
      // Update IN PLACE (二次复审 P3-3): an existing layer keeps its original
      // registration slot instead of being re-pushed to the tail — otherwise
      // priority silently becomes "most recently updated wins" rather than
      // the registration order this module documents.
      const index = prev.findIndex((layer) => layer.id === id);
      if (index === -1) return [...prev, { id, override }];
      if (overrideEqual(prev[index].override, override)) return prev;
      const next = [...prev];
      next[index] = { id, override };
      return next;
    });
  }, []);

  const value = useMemo(() => ({
    layers: layers.map((layer) => layer.override),
    setLayer,
  }), [layers, setLayer]);

  return (
    <MobileHeaderContext.Provider value={value}>
      {children}
    </MobileHeaderContext.Provider>
  );
}

/**
 * Allows a page to bind a route-aware mobile header action/title without
 * coupling route handles to React nodes.
 *
 * The effect must NOT depend on the context object or the options identity:
 * registration goes through a ref, and only the semantic fields are
 * dependencies — an unstable `onAction` or a provider re-render can never
 * loop. Only EXPLICITLY set fields are carried: the shell merges this over
 * the route handle's chrome.mobile, and an explicit `undefined` would erase
 * a handle-declared action/title instead of leaving it alone.
 */
function registerMobileHeaderLayer(options: MobileHeaderOverride | null) {
  const context = useContext(MobileHeaderContext);
  const contextRef = useRef(context);
  contextRef.current = context;
  const idRef = useRef<LayerId | null>(null);
  const { action, onAction } = options ?? {};

  useEffect(() => {
    const ctx = contextRef.current;
    if (!ctx || !options) return undefined;
    if (idRef.current === null) idRef.current = Symbol('mobile-header-layer');
    const id = idRef.current;

    const next: MobileHeaderOverride = {};
    if (action !== undefined) next.action = action;
    if (onAction !== undefined) next.onAction = onAction;

    ctx.setLayer(id, next);
    return () => contextRef.current?.setLayer(id, null);
    // Deliberately only the semantic fields — see the doc comment above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [action, onAction]);
}

/** Field-wise merge of every registered layer (later layers win). */
export function useMobileHeaderState() {
  const layers = useContext(MobileHeaderContext)?.layers ?? EMPTY_LAYERS;
  return useMemo(() => {
    if (layers.length === 0) return null;
    const merged: MobileHeaderOverride = {};
    for (const layer of layers) Object.assign(merged, layer);
    return merged;
  }, [layers]);
}

/**
 * useMobileHeaderAction — 页面绑定顶栏 action 的唯一 API（Architecture 2.0
 * §27/§88：旧泛用 useMobileHeader 已删除）。静态部分（mode/title/showBack）
 * 由 Route Meta 决定，动态层只允许覆盖 action。
 */
export function useMobileHeaderAction(options: MobileHeaderOverride | null) {
  registerMobileHeaderLayer(options);
}

// ── 页面内部 back override（§28） ─────────────────────────────────────

type BackOverride = (() => void) | null;

const MobileBackOverrideContext = createContext<{ backOverride: BackOverride; setBackOverride: (value: BackOverride) => void }>({
  backOverride: null,
  setBackOverride: () => {},
});

export function MobileBackOverrideProvider({ children }: { children: React.ReactNode }) {
  const [backOverride, setBackOverride] = useState<BackOverride>(null);
  const value = useMemo(() => ({ backOverride, setBackOverride }), [backOverride]);
  return (
    <MobileBackOverrideContext.Provider value={value}>
      {children}
    </MobileBackOverrideContext.Provider>
  );
}

/**
 * 页面内部多 step（如自动化编辑器 compose→agent→trigger）接管顶栏返回：
 * 只接受 onBack callback，绝不接受 backTo URL —— 页面不得重新拥有路由父子关系。
 */
export function useMobileBackOverride({ active, onBack }: { active: boolean; onBack: () => void }) {
  const { setBackOverride } = useContext(MobileBackOverrideContext);
  const onBackRef = useRef(onBack);
  onBackRef.current = onBack;

  useEffect(() => {
    if (!active) return undefined;
    setBackOverride(() => onBackRef.current());
    return () => setBackOverride(null);
  }, [active, setBackOverride]);
}

/** Shell 读取当前是否有人接管了返回。 */
export function useBackOverrideState(): BackOverride {
  return useContext(MobileBackOverrideContext).backOverride;
}
