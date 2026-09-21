import { useState } from 'react';
import { DownOutlined } from '@ant-design/icons';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import { ContentSection, EntityRow } from '@/components/ProductUI';
import type { ApplicationSummary, ComposerApplication } from '@/services/runApi';
import CapabilityPicker from '../capability/CapabilityPicker';
import { useIsMobile } from '@/shell/useIsMobile';
import './workbench-home.css';

export default function WorkbenchHome({ current, recommended, recent, onOpen }: { current: ComposerApplication | null; recommended: ApplicationSummary[]; recent: ApplicationSummary[]; onOpen: (item: ApplicationSummary) => void; }) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const isMobile = useIsMobile();
  const renderCapability = (item: ApplicationSummary) => <EntityRow key={item.id} leading={<AgentAvatar application={item} size={32} tint={item.color} />} title={item.name} description={item.description || (item.kind === 'chat' ? '智能体' : '应用')} onClick={() => onOpen(item)} />;
  return <div className="workbench-home">
    <div className="workbench-home__hero">
      {current && <AgentAvatar application={current} size={58} tint={current.color} />}
      <button type="button" className="workbench-home__current" onClick={() => setPickerOpen(true)}>{current?.name || '选择能力'} <DownOutlined /></button>
      <h1>今天想完成什么？</h1><p>选择智能体或应用，然后描述你的任务。</p>
    </div>
    {!isMobile && recent.length > 0 && <ContentSection title="最近使用"><div className="workbench-home__grid">{recent.slice(0, 8).map(renderCapability)}</div></ContentSection>}
    {!isMobile && recommended.length > 0 && <ContentSection title="推荐能力"><div className="workbench-home__grid">{recommended.slice(0, 6).map(renderCapability)}</div></ContentSection>}
    <CapabilityPicker open={pickerOpen} onClose={() => setPickerOpen(false)} />
  </div>;
}
