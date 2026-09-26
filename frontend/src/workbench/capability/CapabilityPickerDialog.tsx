import { Modal } from 'antd';
import CapabilityPickerCore from './CapabilityPickerCore';
import type { ApplicationSummary } from '@/services/runApi';
import type { TaskSummary } from '@/types/task';

export default function CapabilityPickerDialog({ open, onClose, onSelect, onTaskSelect, afterOpenChange }: {
  open: boolean;
  onClose: () => void;
  onSelect: (item: ApplicationSummary) => void;
  onTaskSelect: (task: TaskSummary) => void;
  /** 导航延迟到关闭动画结束（Architecture 2.0 §69）。 */
  afterOpenChange?: (open: boolean) => void;
}) {
  return (
    <Modal open={open} onCancel={onClose} afterOpenChange={afterOpenChange} footer={null} title="选择能力" width={620} destroyOnHidden>
      <CapabilityPickerCore onSelect={onSelect} onTaskSelect={onTaskSelect} />
    </Modal>
  );
}
