import { useNavigate } from 'react-router-dom';
import { routeForApplication } from '@/lib/applicationRoute';
import type { ApplicationSummary } from '@/services/runApi';
import { useIsMobile } from '@/shell/useIsMobile';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import CapabilityPickerDialog from './CapabilityPickerDialog';
import CapabilityPickerSheet from './CapabilityPickerSheet';
import './capability.css';

export default function CapabilityPicker({ open, onClose, onSelect }: { open: boolean; onClose: () => void; onSelect?: (item: ApplicationSummary) => void }) {
  const isMobile = useIsMobile();
  const navigate = useNavigate();
  const openApplication = useWorkspaceStore((state) => state.openApplication);
  const select = (item: ApplicationSummary) => {
    if (onSelect) { onClose(); onSelect(item); return; }
    openApplication(item.id);
    onClose();
    navigate(routeForApplication(item));
  };
  const selectTask = (task: import('@/types/task').TaskSummary) => { onClose(); navigate(task.applicationSlug ? `/chat/${task.applicationSlug}?conversation=${task.id}` : `/?conversation=${task.id}`); };
  const props = { open, onClose, onSelect: select, onTaskSelect: selectTask };
  return isMobile ? <CapabilityPickerSheet {...props} /> : <CapabilityPickerDialog {...props} />;
}
