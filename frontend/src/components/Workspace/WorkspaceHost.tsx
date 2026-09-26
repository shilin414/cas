/**
 * Resolve the route's application before mounting any consumer renderer.
 *
 * The entity cache can accelerate shared reads, but it cannot admit a route:
 * a management page may have cached a disabled/private/unbound application.
 * Only the result of this entry's forced consume `/resolve` may mount the
 * chat/page/workflow surface.
 *
 * POP 快速恢复（Architecture 2.0 §70–§74）：路由变化时先查 consume-validated
 * 快照 —— 存在则同步显示（返回不阻塞），无论有无快照都在后台强制 resolve；
 * revalidate 成功更新/404，权限失败则不再沿用旧页面；网络失败且存在
 * fresh consume 快照时暂时继续显示，否则 Error + Retry。
 */
import React, { useEffect, useState } from 'react';
import { Button, Empty, Spin } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { useApplicationEntityStore, DEFAULT_CONSUME_TTL_MS } from '@/stores/useApplicationEntityStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import type { V2Application } from '@/services/runApi';
import ChatRenderer from './ChatRenderer';
import HomeWorkspace from './HomeWorkspace';
import PageRenderer from './PageRenderer';
import WorkflowRenderer from './WorkflowRenderer';

export type WorkspaceKind = 'home' | 'chat' | 'page' | 'workflow';

interface Props {
  kind?: WorkspaceKind;
}

/** consume-validated 快照：仅 consume 端点完整响应写入（§71）。 */
function consumeValidatedSnapshot(slug: string): V2Application | null {
  const { bySlug, validatedAtById } = useApplicationEntityStore.getState();
  const cached = bySlug[slug];
  if (!cached) return null;
  const validatedAt = validatedAtById[cached.id];
  // 过旧的快照不能作为同步 admitted 起点（宁 loading 不旧页面）。
  if (validatedAt == null || Date.now() - validatedAt > DEFAULT_CONSUME_TTL_MS) return null;
  return cached;
}

const WorkspaceHost: React.FC<Props> = ({ kind = 'home' }) => {
  const navigate = useNavigate();
  const { applicationSlug } = useParams<{ applicationSlug?: string }>();
  const ensureBySlug = useApplicationEntityStore((state) => state.ensureBySlug);
  const setActiveApplication = useWorkspaceStore((state) => state.setActiveApplication);
  const openApplication = useWorkspaceStore((state) => state.openApplication);
  // undefined = resolve pending, null = authoritative 404, object = admitted.
  const [resolvedApplication, setResolvedApplication] = useState<
    V2Application | null | undefined
  >(undefined);
  const [resolvedSlug, setResolvedSlug] = useState<string | null>(null);
  const [resolveFailed, setResolveFailed] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);

  useEffect(() => {
    if (!applicationSlug) {
      setResolvedSlug(null);
      setResolvedApplication(null);
      setResolveFailed(false);
      setActiveApplication(null);
      return undefined;
    }

    let active = true;
    setActiveApplication(null);
    setResolvedSlug(applicationSlug);
    setResolveFailed(false);
    // 同步预加载（§72）：consume-validated 快照存在 → 立即显示；
    // 否则 undefined = loading（null 只能来自 resolve 的权威结论）。
    const snapshot = consumeValidatedSnapshot(applicationSlug);
    setResolvedApplication(snapshot ?? undefined);
    void ensureBySlug(applicationSlug, { maxAgeMs: 0, bypassBackoff: retryNonce > 0 })
      .then((application) => {
        if (!active) return;
        setResolvedApplication(application ?? null);
      })
      .catch(() => {
        if (!active) return;
        // §74：网络失败但进入路由时有 fresh consume 快照 → 暂时继续显示。
        // 注意 ensureBySlug 失败路径会把实体从 store 移除（hideUntilRetry），
        // 所以以进入路由那一刻的快照为准，而不是失败后再查。
        if (snapshot) return;
        setResolveFailed(true);
      });
    return () => { active = false; };
  }, [applicationSlug, ensureBySlug, retryNonce, setActiveApplication]);

  useEffect(() => {
    if (resolvedApplication) openApplication(resolvedApplication.id);
  }, [resolvedApplication, openApplication]);

  if (!applicationSlug) return <HomeWorkspace />;

  // Effects run after render. Associate the admission result with its lookup
  // key so a route change cannot render the previous slug for one frame.
  if (resolvedSlug !== applicationSlug) {
    return <div className="workspace-host__loading"><Spin size="large" /></div>;
  }

  if (resolveFailed) {
    return (
      <div className="workspace-host__missing">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="应用加载失败，请检查网络后重试"
        >
          <Button type="primary" onClick={() => setRetryNonce((value) => value + 1)}>
            重新加载
          </Button>
          <Button onClick={() => navigate('/')}>返回工作台</Button>
        </Empty>
      </div>
    );
  }

  if (resolvedApplication === undefined) {
    return <div className="workspace-host__loading"><Spin size="large" /></div>;
  }

  if (resolvedApplication === null) {
    return (
      <div className="workspace-host__missing">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={`找不到应用：${applicationSlug}`}
        >
          <Button type="primary" onClick={() => navigate('/')}>返回工作台</Button>
        </Empty>
      </div>
    );
  }

  if (kind === 'chat' || resolvedApplication.kind === 'chat'
    || resolvedApplication.renderer_key === 'chat') {
    return (
      <ChatRenderer
        key={resolvedApplication.id}
        application={resolvedApplication}
      />
    );
  }
  if (kind === 'workflow') {
    return <WorkflowRenderer application={resolvedApplication} />;
  }
  return <PageRenderer application={resolvedApplication} />;
};

export default WorkspaceHost;
