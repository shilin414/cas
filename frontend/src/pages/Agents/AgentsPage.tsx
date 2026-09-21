import React, { useState } from 'react';
import { Button, message } from 'antd';
import { ArrowRightOutlined, StarFilled, StarOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import {
  EmptyState,
  EntityCard,
  IconButton,
  LoadingState,
  MetaLine,
  PageHeader,
  PageSurface,
  PageToolbar,
  SearchField,
  StatusBadge,
} from '@/components/ProductUI';
import { useApplicationPage } from '@/hooks/useApplicationPage';
import { useCatalogUiStore } from '@/stores/useCatalogUiStore';
import { applicationConsumeBlock } from '@/lib/applicationConsumability';
import { setApplicationFavorite, type V2Application } from '@/services/runApi';
import { useIsMobile } from '@/shell/useIsMobile';
import MobileAgentCenter from './MobileAgentCenter';
import './AgentsPage.css';

const PAGE_SIZE = 24;

const DesktopAgentCenter: React.FC = () => {
  const navigate = useNavigate();
  const category = useCatalogUiStore((state) => state.agentCategory);
  const [query, setQuery] = useState('');
  const { items, loading, loadingMore, hasMore, loadMore, patchItem } = useApplicationPage({
    kind: 'chat', scope: 'accessible', mode: 'consume', category, query, limit: PAGE_SIZE,
  });

  const toggleFavorite = async (agent: V2Application) => {
    const next = !agent.is_favorite;
    try {
      await setApplicationFavorite(agent.id, next);
      patchItem(agent.id, { is_favorite: next });
    } catch {
      message.error('收藏操作失败');
    }
  };

  const open = (agent: V2Application) => {
    const block = applicationConsumeBlock(agent);
    if (block) {
      message.warning(block);
      return;
    }
    navigate(`/chat/${encodeURIComponent(agent.slug)}`);
  };

  return (
    <PageSurface width="wide" className="agents-page">
      <PageHeader title="智能体中心" description="发现、收藏并使用企业已授权的智能体" />
      <PageToolbar search={<SearchField value={query} onChange={setQuery} onClear={() => setQuery('')} placeholder="搜索智能体" />} />
      {loading && items.length === 0 ? (
        <LoadingState label="正在加载智能体…" rows={6} />
      ) : items.length === 0 ? (
        <EmptyState title="暂无可用智能体" description="请调整搜索条件，或联系管理员授权可用智能体。" />
      ) : (
        <>
          <div className="agent-grid">
            {items.map((agent) => (
              <EntityCard
                key={agent.id}
                className="agent-card"
                leading={<AgentAvatar application={agent} size={48} />}
                title={agent.name}
                description={agent.description || '暂无描述'}
                meta={<MetaLine><span>{agent.provider_key || '企业智能体'}</span>{agent.category_name && <StatusBadge>{agent.category_name}</StatusBadge>}{agent.is_default_agent && <StatusBadge tone="warning">默认</StatusBadge>}</MetaLine>}
                trailing={(
                  <IconButton
                    label={agent.is_favorite ? '取消收藏' : '收藏'}
                    icon={agent.is_favorite ? <StarFilled className="catalog-favorite-icon" /> : <StarOutlined />}
                    onClick={() => void toggleFavorite(agent)}
                  />
                )}
                footer={<span className="agent-card__footer-copy">打开对话 <ArrowRightOutlined aria-hidden="true" /></span>}
                onClick={() => open(agent)}
                ariaLabel={`打开智能体 ${agent.name}`}
              />
            ))}
          </div>
          <div className="agents-page__more">
            {hasMore ? <Button loading={loadingMore} onClick={() => void loadMore()}>加载更多智能体</Button> : items.length > PAGE_SIZE && <span className="agents-page__count">已显示全部 {items.length} 个</span>}
          </div>
        </>
      )}
    </PageSurface>
  );
};

const AgentsPage: React.FC = () => {
  const isMobile = useIsMobile();
  return isMobile ? <MobileAgentCenter /> : <DesktopAgentCenter />;
};
export default AgentsPage;
