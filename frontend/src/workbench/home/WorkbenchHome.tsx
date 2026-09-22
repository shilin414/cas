import { useState } from 'react';
import { DownOutlined } from '@ant-design/icons';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import type { ApplicationSummary, ComposerApplication } from '@/services/runApi';
import CapabilityPicker from '../capability/CapabilityPicker';
import { useIsMobile } from '@/shell/useIsMobile';
import './workbench-home.css';

export default function WorkbenchHome({ current, onOpen }: { current: ComposerApplication | null; onOpen: (item: ApplicationSummary) => void; }) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const isMobile = useIsMobile();
  return <div className={`workbench-home${isMobile ? '' : ' workbench-home--desktop'}`}>
    {isMobile ? <div className="workbench-home__hero">
      {current && <AgentAvatar application={current} size={58} tint={current.color} />}
      <button type="button" className="workbench-home__current" onClick={() => setPickerOpen(true)}>{current?.name || '选择能力'} <DownOutlined /></button>
      <h1>今天想完成什么？</h1><p>选择智能体或应用，然后描述你的任务。</p>
    </div> : <div className="agent-home-identity">
      {current && <AgentAvatar application={current} size={60} tint={current.color} />}
      <div className="agent-home-identity__body">
        <h1><button type="button" className="workbench-home__current" aria-haspopup="dialog" onClick={() => setPickerOpen(true)}>{current?.name || '选择智能体'} <DownOutlined /></button></h1>
        <p>{current?.description || '描述你的任务，让智能体协助你完成工作。'}</p>
      </div>
    </div>}
    <CapabilityPicker open={pickerOpen} onClose={() => setPickerOpen(false)} onSelect={isMobile ? undefined : onOpen} />
  </div>;
}
