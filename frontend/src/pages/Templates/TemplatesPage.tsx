import React, { useEffect, useRef } from 'react';
import { ArrowRightOutlined, ClockCircleOutlined, FileTextOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { EmptyState, LoadingState, MediaCard, MetaLine, PageHeader, PageSurface, PageToolbar, SearchField, StatusBadge, TagGroup } from '@/components/ProductUI';
import { useTemplateStore } from '@/stores/useTemplateStore';
import './TemplatesPage.css';

const TYPE_ICONS: Record<string, string> = { article: '✍️', social_post: '📱', video_script: '🎬', brand_story: '🏷️', podcast: '🎙️', product_analysis: '🔍', other: '📄' };
const formatSource = (platform?: string, author?: string) => [platform, author].filter(Boolean).join(' · ') || '来源待补充';
const TemplatesPage: React.FC = () => {
  const navigate = useNavigate();
  const { templates, isLoading, selectedCategory, searchQuery, loadTemplates, setSearchQuery } = useTemplateStore();
  const isFirstRun = useRef(true);
  useEffect(() => { if (isFirstRun.current) { isFirstRun.current = false; loadTemplates(selectedCategory || undefined); return; } const timer = window.setTimeout(() => loadTemplates(selectedCategory || undefined), 300); return () => window.clearTimeout(timer); }, [selectedCategory, searchQuery, loadTemplates]);
  return <PageSurface width="wide" className="templates-page">
    <PageHeader title="案例库" description="阅读真实案例，理解内容结构、表达方法与可复用规律" meta={`${templates.length} 个案例`} />
    <PageToolbar search={<SearchField value={searchQuery} onChange={setSearchQuery} onClear={() => setSearchQuery('')} placeholder="搜索标题、作者、平台或方法" />} />
    {isLoading ? <LoadingState label="正在加载案例…" rows={6} /> : templates.length === 0 ? <EmptyState title="暂无匹配案例" description="尝试更换关键词或分类。" /> : <div className="template-grid">{templates.map((template) => <MediaCard
      key={template.id}
      className="template-card"
      media={<div className="template-card__visual">{template.thumbnail ? <img src={template.thumbnail} alt="" /> : <span aria-hidden="true">{TYPE_ICONS[template.content_type] || TYPE_ICONS.other}</span>}<div className="template-card__badges"><StatusBadge>{template.content_type_display}</StatusBadge>{template.is_featured && <StatusBadge tone="warning">精选</StatusBadge>}</div></div>}
      meta={formatSource(template.source_platform, template.source_author)}
      title={template.title}
      description={template.summary || template.recommended_reason}
      tags={<TagGroup>{(template.tags || []).slice(0, 3).map((tag) => <StatusBadge key={tag}>{tag}</StatusBadge>)}</TagGroup>}
      actions={<><MetaLine><span><FileTextOutlined /> {template.analysis_count} 项拆解</span>{template.reading_time_minutes > 0 && <span><ClockCircleOutlined /> {template.reading_time_minutes} 分钟</span>}</MetaLine><span className="template-card__open">查看 <ArrowRightOutlined /></span></>}
      onClick={() => navigate(`/templates/${template.id}`)}
      ariaLabel={`查看案例 ${template.title}`}
    />)}</div>}
  </PageSurface>;
};
export default TemplatesPage;
