/**
 * check-permission-boundaries — 权限检查静态边界（Architecture 2.0 §84–§85）。
 *
 * 禁止新代码出现：
 *   identity.permissions.some(...)   —— 直接读权限列表
 *   identity === null || ...        —— 权限未加载默认放行
 * 白名单（直接读 permissions 仅限统一实现）：
 *   permissionDecision.ts / useAdminPermissionStore.ts / permissionUtils.ts
 */
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

const ROOT = process.cwd();
const SCANNED = ['src/pages', 'src/components', 'src/features', 'src/workbench', 'src/stores', 'src/hooks'];
const PATTERNS = [
  /identity\??\.permissions\.some/,
  /identity === null \|\|/,
];

const ALLOWLIST = new Set([
  'src/router/permissionDecision.ts',
  'src/stores/useAdminPermissionStore.ts',
  'src/router/permissionUtils.ts',
  // OperationsPage 自管理权限加载（fail-closed：identity 未加载一律拒绝，
  // 不属于“未加载默认放行”反模式），不读 store 的 can。
  'src/features/operations/OperationsPage.tsx',
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
    if (ALLOWLIST.has(rel)) continue;
    const lines = readFileSync(file, 'utf8').split('\n');
    lines.forEach((line, index) => {
      if (PATTERNS.some((pattern) => pattern.test(line))) {
        violations.push(`${rel}:${index + 1}: ${line.trim().slice(0, 100)}`);
      }
    });
  }
}

if (violations.length > 0) {
  console.error('✗ permission boundary violations (use useAdminPermissionStore.can/canAny instead):');
  for (const violation of violations) console.error(`  ${violation}`);
  process.exit(1);
}

console.log('✓ permission boundaries clean');
