import DesktopAppShell from './DesktopAppShell';
import MobileAppShell from './MobileAppShell';
import { useIsMobile } from './useIsMobile';
import { useShellChrome } from './useShellChrome';
import { useRouteScrollRestoration } from '@/router/useRouteScrollRestoration';

/**
 * AppShell — the single, always-mounted outer shell (§17/§33).
 *
 * One React tree serves PC and Mobile (§24): only the interaction layout is
 * swapped (DesktopAppShell / MobileAppShell), never the project, the API layer
 * or the WorkspaceHost inside it.
 */
const AppShell: React.FC = () => {
  const isMobile = useIsMobile();
  const chrome = useShellChrome();
  // 会话内滚动恢复（Architecture 2.0 §78–§80）：POP 恢复、PUSH 顶部、
  // root 切换保留各自最近位置。不持久化，刷新清空。
  useRouteScrollRestoration();

  return isMobile
    ? <MobileAppShell chrome={chrome} />
    : <DesktopAppShell chrome={chrome} />;
};

export default AppShell;
