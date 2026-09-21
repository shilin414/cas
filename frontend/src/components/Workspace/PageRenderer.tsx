/** Fixed business applications share the workspace shell and catalog ACL entry. */
import React, { useEffect } from 'react';
import { Button, Empty } from 'antd';
import { ArrowLeftOutlined, StarFilled, StarOutlined } from '@ant-design/icons';
import { useLocation, useNavigate } from 'react-router-dom';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import type { V2Application } from '@/services/runApi';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import BusinessApp from '@/components/BusinessApps/BusinessApp';

interface Props {
  application: V2Application;
  /** Label of the workspace kind shown in the header (页面 / 工作流). */
  kindLabel?: string;
}

/** A fixed-application page implementation (drops in as business logic lands). */
type FixedAppRenderer = React.ComponentType<{ application: V2Application; snapshotToken?: string; onNewQuery?: () => void }>;

/** Code-deployed business pages; keys must match catalog renderer_key values. */
const FIXED_RENDERERS: Record<string, FixedAppRenderer> = {
  'barcode-query': BusinessApp,
  'oa-unlock': BusinessApp,
  'oa-password': BusinessApp,
  'material-query': BusinessApp,
  'ldap-password': BusinessApp,
  'oa-phone': BusinessApp,
  'tpm-account': BusinessApp,
};

const PageRenderer: React.FC<Props> = ({ application, kindLabel = '应用' }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const snapshotToken = new URLSearchParams(location.search).get('result') || undefined;
  const isStaff = useAuthStore((state) => Boolean(state.user?.is_staff));
  // The favourite flag is patched into the bootstrap groups and the entity
  // cache; `application` is handed over as the fallback so an application
  // that is in no group can still be starred (执行报告 §15).
  const toggleFavorite = useWorkspaceBootstrapStore((state) => state.toggleFavorite);
  const openApplication = useWorkspaceStore((state) => state.openApplication);
  const previousApplicationId = useWorkspaceStore((state) => state.previousApplicationId);

  useEffect(() => { openApplication(application.id); }, [application.id, openApplication]);

  /** §80: back goes to the workspace the user came from, never a reload. */
  const goBack = () => {
    // The previous application is in the entity cache because entering it is
    // what resolved it — a cache miss falls back to the home workspace rather
    // than triggering a catalog download (执行报告 §9).
    const previous = useApplicationEntityStore.getState().get(previousApplicationId);
    if (previous) {
      navigate(previous.kind === 'chat'
        ? `/chat/${previous.slug}`
        : `/${previous.kind === 'workflow' ? 'workflow' : 'app'}/${previous.slug}`);
    } else {
      navigate('/');
    }
  };

  const Renderer = FIXED_RENDERERS[application.renderer_key || ''];

  return (
    <div className="workspace-host">
      <header className="chat-renderer__head">
        <Button
          type="text"
          size="small"
          icon={<ArrowLeftOutlined />}
          onClick={goBack}
        >
          返回工作台
        </Button>
        <AgentAvatar application={application} size={22} shape="circle" />
        <strong>{application.name}</strong>
        <span className="home-shortcut__desc">
          {kindLabel}
          {application.renderer_key ? ` · ${application.renderer_key}` : ''}
        </span>
        {isStaff && !application.enabled && (
          <span className="home-shortcut__desc">已停用（仅管理员可见）</span>
        )}
        <span className="chat-renderer__head-spacer" />
        <Button
          type="text"
          aria-label={application.is_favorite ? '取消收藏' : '收藏'}
          icon={application.is_favorite ? <StarFilled /> : <StarOutlined />}
          onClick={() => void toggleFavorite(application.id, application)}
        />
      </header>
      <div className="chat-renderer__body">
        {Renderer ? (
          <Renderer key={`${application.id}:${snapshotToken || ''}`} application={application} snapshotToken={snapshotToken} onNewQuery={() => navigate(`/app/${application.slug}`)} />
        ) : (
          <div className="workspace-host__missing">
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                application.renderer_key
                  ? `${application.name} 尚未注册界面：${application.renderer_key}`
                  : `${application.name} 尚未配置页面`
              }
            />
          </div>
        )}
      </div>
    </div>
  );
};

export default PageRenderer;
