import { useNavigate } from 'react-router-dom';
import { routeForApplication } from '@/lib/applicationRoute';
import type { ApplicationSummary } from '@/services/runApi';
import { useIsMobile } from '@/shell/useIsMobile';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import { useOverlayNavigation } from '@/router/useOverlayNavigation';
import CapabilityPickerDialog from './CapabilityPickerDialog';
import CapabilityPickerSheet from './CapabilityPickerSheet';
import './capability.css';

/**
 * CapabilityPicker — 能力选择（Picker 类 Overlay，不 Route 化）。
 *
 * 导航时序（Architecture 2.0 §69）：选中项先记录目标并关闭抽屉/弹窗，
 * afterOpenChange(false) 动画结束后才 navigate —— 不用 setTimeout 猜。
 */
export default function CapabilityPicker({ open, onClose, onSelect }: { open: boolean; onClose: () => void; onSelect?: (item: ApplicationSummary) => void }) {
  const isMobile = useIsMobile();
  const navigate = useNavigate();
  const openApplication = useWorkspaceStore((state) => state.openApplication);
  const { requestNavigation, afterOpenChange } = useOverlayNavigation(navigate);

  const select = (item: ApplicationSummary) => {
    if (onSelect) { onClose(); onSelect(item); return; }
    openApplication(item.id);
    requestNavigation(routeForApplication(item));
    onClose();
  };

  const selectTask = (task: import('@/types/task').TaskSummary) => {
    requestNavigation(task.applicationSlug ? `/chat/${task.applicationSlug}?conversation=${task.id}` : `/?conversation=${task.id}`);
    onClose();
  };

  const props = { open, onClose, onSelect: select, onTaskSelect: selectTask, afterOpenChange };
  return isMobile ? <CapabilityPickerSheet {...props} /> : <CapabilityPickerDialog {...props} />;
}
