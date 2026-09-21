import React, { useState } from 'react';
import { Button } from 'antd';
import { ArrowRightOutlined } from '@ant-design/icons';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import { useNavigate } from 'react-router-dom';
import {
  EmptyState,
  LoadingState,
  PageHeader,
  PageSurface,
  PageToolbar,
  SearchField,
  StatusBadge,
  TagGroup,
} from '@/components/ProductUI';
import { useCatalogUiStore } from '@/stores/useCatalogUiStore';
import { useApplicationPage } from '@/hooks/useApplicationPage';
import type { V2Application } from '@/services/runApi';
import { useIsMobile } from '@/shell/useIsMobile';
import MobileAppCenter from './MobileAppCenter';
import './AppsPage.css';

const KIND_LABELS: Record<string, string> = { page: '页面', form: '表单', dashboard: '看板', custom: '应用', task: '任务' };

const DesktopAppCenter: React.FC = () => {
  const navigate = useNavigate();
  const category = useCatalogUiStore((state) => state.fixedCategory);
  const [query, setQuery] = useState('');
  const { items, loading, loadingMore, hasMore, loadMore } = useApplicationPage({ kind: 'fixed', scope: 'accessible', mode: 'consume', category, query, limit: 24 });
  const open = (app: V2Application) => navigate(`/app/${encodeURIComponent(app.slug)}`);

  return (
    <PageSurface width="wide" className="apps-page">
      <PageHeader title="应用中心" description="发现并打开企业已授权的固定应用" />
      <PageToolbar search={<SearchField value={query} onChange={setQuery} onClear={() => setQuery('')} placeholder="搜索应用" />} />
      {loading && items.length === 0 ? (
        <LoadingState label="正在加载应用…" rows={6} />
      ) : items.length === 0 ? (
        <EmptyState title="暂无可用应用" description="请调整搜索条件，或联系管理员授权应用。" />
      ) : (
        <>
          <div className="app-grid">
            {items.map((app) => (
              <button
                key={app.id}
                type="button"
                className="app-card"
                onClick={() => open(app)}
                aria-label={`打开应用 ${app.name}`}
              >
                <span className="app-card__header">
                  <AgentAvatar application={app} size={48} shape="square" tint={app.color} className="app-card__icon" />
                  <span className="app-card__title-wrap">
                    <strong className="app-card__name">{app.name}</strong>
                  </span>
                  <ArrowRightOutlined className="app-card__arrow" aria-hidden="true" />
                </span>
                <span className="app-card__description">{app.description || '暂无描述'}</span>
                <span className="app-card__footer">
                  <TagGroup>{app.category_name && <StatusBadge tone="info">{app.category_name}</StatusBadge>}<StatusBadge>{KIND_LABELS[app.kind] || app.kind}</StatusBadge></TagGroup>
                  <span className="app-card__availability">企业授权</span>
                </span>
              </button>
            ))}
          </div>
          <div className="apps-page__more">
            {hasMore ? <Button loading={loadingMore} onClick={() => void loadMore()}>加载更多应用</Button> : items.length > 24 && <span className="apps-page__count">已显示全部 {items.length} 个</span>}
          </div>
        </>
      )}
    </PageSurface>
  );
};

const AppsPage: React.FC = () => {
  const isMobile = useIsMobile();
  return isMobile ? <MobileAppCenter /> : <DesktopAppCenter />;
};
export default AppsPage;
