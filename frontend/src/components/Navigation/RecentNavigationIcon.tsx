import { AppstoreOutlined, MessageOutlined, RobotOutlined } from '@ant-design/icons';
import { useNavigationPreferencesStore } from '@/stores/useNavigationPreferencesStore';

/** Sidebar-only presentation; does not change application avatars elsewhere. */
export default function RecentNavigationIcon({ kind, emoji, task = false }: {
  kind?: string;
  emoji?: string;
  task?: boolean;
}) {
  const mode = useNavigationPreferencesStore((state) => state.iconMode);
  if (mode === 'hidden') return null;
  const Icon = task ? MessageOutlined : kind === 'chat' ? RobotOutlined : AppstoreOutlined;
  return (
    <span className="sidebar-navigation-icon" aria-hidden="true">
      {mode === 'emoji' ? (emoji || (task ? '💬' : kind === 'chat' ? '🤖' : '🧩')) : <Icon />}
    </span>
  );
}
