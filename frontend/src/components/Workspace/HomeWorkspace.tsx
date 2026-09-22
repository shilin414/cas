/**
 * Agent landing workspace. Selecting a chat agent stays on the desktop home;
 * fixed applications retain their existing routes. Merely visiting or selecting
 * creates no conversation or run: creation remains lazy, on the first send.
 * Bootstrap resolves the default agent; explicit selection resolves one entity.
 * Historical conversation deep links still open the owning chat workspace.
 */
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Empty, Spin, message } from 'antd';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { routeForApplication } from '@/lib/applicationRoute';
import { api } from '@/services/api';
import { RunChatPanel } from '@/components/Chat';
import type { SendDecision } from '@/components/Chat/RunChatPanel';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import type { ApplicationSummary, ComposerApplication } from '@/services/runApi';
import WorkbenchHome from '@/workbench/home/WorkbenchHome';
import AgentWorkspaceCollections from '@/workbench/home/AgentWorkspaceCollections';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useIsMobile } from '@/shell/useIsMobile';
import './HomeWorkspace.css';

const HomeWorkspace: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const navigationKey = location.key ?? 'default';
  const navigationState: unknown = location.state;
  const rawApplicationId = navigationState && typeof navigationState === 'object'
    && 'newTaskApplicationId' in navigationState ? navigationState.newTaskApplicationId : null;
  const [searchParams, setSearchParams] = useSearchParams();
  // Mobile swaps ONLY the empty state (§6.1): the WorkspaceHost, the Run API
  // and this component's deep-link / @mention handling are shared, so the two
  // shells can never drift on what a shortcut does — only on how it looks.
  const bootstrapLoading = useWorkspaceBootstrapStore((state) => state.isLoading);
  const loadBootstrap = useWorkspaceBootstrapStore((state) => state.load);
  const defaultApplication = useWorkspaceBootstrapStore((state) => state.defaultApplication);
  const isMobile = useIsMobile();
  // Carry identity only. Never inherit the previous task's conversation or input.
  const requestedApplicationId = !searchParams.get('conversation')
    && typeof rawApplicationId === 'number' && Number.isSafeInteger(rawApplicationId) && rawApplicationId > 0
    ? rawApplicationId : null;
  const [selection, setSelection] = useState<{
    key: string;
    application: ComposerApplication | null;
    status: 'loading' | 'ready' | 'error';
    error?: string;
  }>({ key: navigationKey, application: null, status: requestedApplicationId ? 'loading' : 'ready' });
  const [selectionRetry, setSelectionRetry] = useState(0);
  const selectionVersion = useRef(0);
  const invalidateSelection = useCallback(() => ++selectionVersion.current, []);
  const currentApplication = (selection.key === navigationKey ? selection.application : null) ?? defaultApplication;
  const selectionPending = requestedApplicationId != null
    && (selection.key !== navigationKey || selection.status === 'loading');
  const drafts = useRef<Record<number, string>>({});
  const ensureApplication = useApplicationEntityStore((state) => state.ensure);
  const openApplication = useWorkspaceStore((state) => state.openApplication);
  const rememberConversation = useWorkspaceStore((state) => state.rememberConversation);
  const [resolving, setResolving] = useState(false);
  /** Distinguishes "no such conversation/application" from "offline" (P1-6). */
  const [resolveFailed, setResolveFailed] = useState(false);

  useEffect(() => { void loadBootstrap(); }, [loadBootstrap]);

  useEffect(() => {
    const version = invalidateSelection();
    drafts.current = {};
    if (requestedApplicationId == null) {
      setSelection({ key: navigationKey, application: null, status: 'ready' });
    } else {
      setSelection({ key: navigationKey, application: null, status: 'loading' });
      void ensureApplication(requestedApplicationId, { maxAgeMs: 0, bypassBackoff: selectionRetry > 0 })
        .then((application) => {
          if (version !== selectionVersion.current) return;
          if (!application || application.is_consumable === false
            || (application.kind !== 'chat' && application.renderer_key !== 'chat')) {
            setSelection({ key: navigationKey, application: null, status: 'error', error: '该智能体暂不可用，请重试或选择默认智能体。' });
            return;
          }
          useRunChatStore.getState().setActiveConversation(null);
          openApplication(application.id);
          setSelection({ key: navigationKey, application, status: 'ready' });
        })
        .catch(() => {
          if (version === selectionVersion.current) {
            setSelection({ key: navigationKey, application: null, status: 'error', error: '智能体加载失败，请检查网络后重试。' });
          }
        });
    }
    return () => { invalidateSelection(); };
  }, [navigationKey, requestedApplicationId, selectionRetry, ensureApplication, openApplication, invalidateSelection]);

  const paramConversation = searchParams.get('conversation');
  const conversationParam = paramConversation
    && Number.isInteger(Number(paramConversation)) ? paramConversation : null;

  // `/?conversation=N` is the history-sidebar deep link. It carries no
  // application, so resolve the owning application — ONE row, by id
  // (执行报告 §13) — and hand over to the real chat workspace. The home
  // workspace itself never renders history.
  useEffect(() => {
    if (!conversationParam) return undefined;
    // The "no application" branch must only run once the bootstrap has
    // landed, otherwise the ?conversation param is dropped on a race.
    if (bootstrapLoading) return undefined;
    let active = true;
    setResolving(true);
    setResolveFailed(false);
    api.get<any>(`/conversations/${conversationParam}/`)
      .then(async (detail) => {
        if (!active) return;
        let target: Awaited<ReturnType<typeof ensureApplication>>;
        try {
          target = await ensureApplication(detail?.application_id);
        } catch {
          // A 500 / timeout / offline backend (二次复审 P1-6). The old code
          // swallowed this together with a 404 and then DELETED the user's
          // `?conversation=` deep link, so a transient network error
          // silently destroyed real navigation state. Keep the URL, show a
          // retry.
          if (active) setResolveFailed(true);
          return;
        }
        if (!active) return;
        if (target) {
          openApplication(target.id);
          rememberConversation(target.id, Number(conversationParam));
          navigate(`/chat/${target.slug}?conversation=${conversationParam}`,
            { replace: true });
          return;
        }
        // No application (legacy conversation): drop back to the idle shell.
        setSearchParams({}, { replace: true });
      })
      .catch(() => {
        // The conversation detail itself failed. Also not a 404: keep the
        // deep link and offer a retry.
        if (active) setResolveFailed(true);
      })
      .finally(() => { if (active) setResolving(false); });
    return () => { active = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationParam, bootstrapLoading, ensureApplication]);

  const handleRouteSend = async (raw: string): Promise<SendDecision> => ({
    action: 'send',
    content: raw,
  });

  const openApplicationWorkspace = async (application: ApplicationSummary) => {
    const version = invalidateSelection();
    if (isMobile || application.kind !== 'chat') {
      openApplication(application.id);
      navigate(routeForApplication(application));
      return;
    }
    try {
      const resolved = await ensureApplication(application.id, { maxAgeMs: 0 });
      if (version !== selectionVersion.current) return;
      if (!resolved) { message.error('该智能体暂不可用'); return; }
      useRunChatStore.getState().setActiveConversation(null);
      setSelection({ key: navigationKey, application: resolved, status: 'ready' });
      openApplication(resolved.id);
    } catch { if (version === selectionVersion.current) message.error('切换智能体失败，请重试'); }
  };

  if (resolving || selectionPending) {
    return (
      <div className="workspace-host__loading"><Spin size="large" /></div>
    );
  }

  if (requestedApplicationId != null && selection.key === navigationKey && selection.status === 'error') {
    return <div className="workspace-host__missing">
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={selection.error}>
        <Button type="primary" onClick={() => setSelectionRetry(value => value + 1)}>重新加载</Button>
        <Button onClick={() => navigate('/', { replace: true, state: null })}>使用默认智能体</Button>
      </Empty>
    </div>;
  }

  // A FAILED deep-link lookup keeps the `?conversation=` param and offers a
  // reload (二次复审 P1-6) — the URL is the user's navigation state, so a
  // transport error must not be "handled" by deleting it.
  if (resolveFailed) {
    return (
      <div className="workspace-host__missing">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="任务加载失败，请检查网络后重试"
        >
          <Button type="primary" onClick={() => window.location.reload()}>
            重新加载
          </Button>
        </Empty>
      </div>
    );
  }

  return (
    <div className="workspace-host">
      <RunChatPanel
        key={`${navigationKey}:${currentApplication?.id ?? 'loading'}`}
        applicationId={currentApplication?.id}
        homeWorkspace
        afterComposer={currentApplication && <AgentWorkspaceCollections key={currentApplication.id} applicationId={currentApplication.id} slug={currentApplication.slug} />}
        draftText={currentApplication ? drafts.current[currentApplication.id] : undefined}
        onDraftChange={text => { if (currentApplication) drafts.current[currentApplication.id] = text; }}
        application={currentApplication ?? undefined}
        conversationId={null}
        onRouteSend={handleRouteSend}
        title="今天想完成什么？"
        description="选择一个能力，或直接描述你的任务开始。"
        emptyState={(
          <WorkbenchHome
            current={currentApplication}
            onOpen={openApplicationWorkspace}
          />
        )}
        onConversationCreated={(id) => {
          if (!currentApplication) return;
          drafts.current[currentApplication.id] = '';
          rememberConversation(currentApplication.id, id);
          navigate(`/chat/${currentApplication.slug}?conversation=${id}`,
            { replace: true });
        }}
      />
    </div>
  );
};

export default HomeWorkspace;
