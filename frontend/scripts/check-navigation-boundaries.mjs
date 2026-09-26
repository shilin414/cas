/**
 * check-navigation-boundaries — 导航架构静态检查（Architecture 2.0 §81–§83）。
 *
 * 禁止在 src/pages / src/components / src/workbench 新增：
 *   useNavigate / window.history / history.back / pushState / replaceState
 * 白名单：src/router/**、src/shell/** 及 baseline 允许列表（存量逐步收敛）。
 *
 * 退出码：0 = 通过；1 = 出现白名单外的违规。
 */
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

const ROOT = process.cwd();
const SCANNED = ['src/pages', 'src/components', 'src/workbench'];
const PATTERNS = [
  /useNavigate\s*\(/,
  /window\.history/,
  /history\.back\s*\(/,
  /\bpushState\b/,
  /\breplaceState\b/,
];

// baseline 允许列表（§82）：存量文件，逐步清空。
// 格式：posix 风格相对路径。
const BASELINE_ALLOWLIST = new Set([
  'src/components/Workspace/ChatRenderer.tsx',
  'src/components/AccountMenu/AccountMenu.tsx',
  'src/components/Agents/AgentDetailModal.tsx',
  'src/components/Header/Header.tsx',
  'src/components/Mobile/MobileHomeSurface.tsx',
  'src/components/Sidebar/AppHistorySidebar.tsx',
  'src/components/Sidebar/ProjectListSidebar.tsx',
  'src/components/Sidebar/Sidebar.tsx',
  'src/components/Workspace/ApplicationSwitcher.tsx',
  'src/components/Workspace/HomeWorkspace.tsx',
  'src/components/Workspace/PageRenderer.tsx',
  'src/components/Workspace/WorkspaceHost.tsx',
  'src/pages/Agents/AgentDetailPage.tsx',
  'src/pages/Agents/AgentsPage.tsx',
  'src/pages/Agents/MobileAgentCenter.tsx',
  'src/pages/Apps/AppDetailPage.tsx',
  'src/pages/Apps/AppsPage.tsx',
  'src/pages/Apps/ApplicationRuntimePage.tsx',
  'src/pages/Apps/AppRunnerPage.tsx',
  'src/pages/Apps/BatchTranscribeRunner.tsx',
  'src/pages/Apps/ImageGenieRunner.tsx',
  'src/pages/Apps/MobileAppCenter.tsx',
  'src/pages/Apps/ChatApplicationEditPage.tsx',
  'src/pages/Auth/AdminLoginPage.tsx',
  'src/pages/Auth/FeishuCallbackPage.tsx',
  'src/pages/Auth/RegisterPage.tsx',
  'src/pages/Auth/SsoCallbackPage.tsx',
  'src/pages/Enterprise/desktop/ResourcePage.tsx',
  'src/pages/Enterprise/desktop/DesktopEnterpriseLayout.tsx', // PC 企业内部导航（§41 允许）
  'src/pages/Enterprise/mobile/MobileAccessPage.tsx',
  'src/pages/Enterprise/mobile/MobileEnterpriseHome.tsx',
  'src/pages/Enterprise/mobile/MobileResourcePage.tsx',
  'src/pages/Schedules/DesktopScheduleCenter.tsx',
  'src/pages/Schedules/ScheduleDetailRoute.tsx',
  'src/pages/Schedules/ScheduleEditorRoute.tsx',
  'src/pages/Skills/SkillsPage.tsx',
  'src/pages/Templates/TemplateDetailPage.tsx',
  'src/pages/Templates/TemplatesPage.tsx',
  'src/pages/Workflows/WorkflowEditorPage.tsx',
  'src/pages/Workflows/WorkflowRunnerPage.tsx',
  'src/pages/Workflows/WorkflowsPage.tsx',
  'src/pages/Workspace/WorkspacePage.tsx',
  'src/workbench/capability/CapabilityPicker.tsx',
  'src/workbench/home/AgentWorkspaceCollections.tsx',
  'src/workbench/shell/DesktopSidebar.tsx',
  'src/workbench/tasks/RecentTaskList.tsx',
  'src/workbench/tasks/TaskCenterPage.tsx',
]);

function walk(dir, files = []) {
  if (!existsSync(dir)) return files;
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, files);
    else if (/\.(ts|tsx)$/.test(entry) && !/\.test\.(ts|tsx)$/.test(entry)) files.push(full);
  }
  return files;
}

const violations = [];
for (const base of SCANNED) {
  for (const file of walk(join(ROOT, base))) {
    const rel = relative(ROOT, file).split(sep).join('/');
    const content = readFileSync(file, 'utf8');
    const lines = content.split('\n');
    lines.forEach((line, index) => {
      if (PATTERNS.some((pattern) => pattern.test(line))) {
        if (!BASELINE_ALLOWLIST.has(rel)) {
          violations.push(`${rel}:${index + 1}: ${line.trim().slice(0, 100)}`);
        } else if (process.env.VERBOSE_BASELINE) {
          console.log(`[baseline] ${rel}:${index + 1}`);
        }
      }
    });
  }
}

if (violations.length > 0) {
  console.error('✗ navigation boundary violations (new files must use useAppNavigation):');
  for (const violation of violations) console.error(`  ${violation}`);
  console.error(`\n${violations.length} violation(s). Allowed dirs: src/router, src/shell;` +
    ' see BASELINE_ALLOWLIST in scripts/check-navigation-boundaries.mjs.');
  process.exit(1);
}

console.log('✓ navigation boundaries clean'
  + ` (baseline allowlist: ${BASELINE_ALLOWLIST.size} files)`);
