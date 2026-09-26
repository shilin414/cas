import { Drawer } from 'antd';
import CapabilityPickerCore from './CapabilityPickerCore';
import type { ApplicationSummary } from '@/services/runApi';
import type { TaskSummary } from '@/types/task';

export default function CapabilityPickerSheet({ open, onClose, onSelect, onTaskSelect, afterOpenChange }: {
  open: boolean;
  onClose: () => void;
  onSelect: (item: ApplicationSummary) => void;
  onTaskSelect: (task: TaskSummary) => void;
  /** 导航延迟到关闭动画结束（Architecture 2.0 §69）。 */
  afterOpenChange?: (open: boolean) => void;
}) {
  return (
    <Drawer open={open} onClose={onClose} afterOpenChange={afterOpenChange} placement="bottom" height="82dvh" title="选择能力" destroyOnHidden>
      <CapabilityPickerCore onSelect={onSelect} onTaskSelect={onTaskSelect} />
    </Drawer>
  );
}
