import { describe, expect, it } from 'vitest';
import { groupAdminPermissions } from '../permissionCatalog';

describe('groupAdminPermissions', () => {
  it('groups permissions into understandable business modules', () => {
    const groups = groupAdminPermissions([
      { id: 2, code: 'directory.sync.manage', category: 'directory', name: '管理同步', description: '配置同步', created_at: '' },
      { id: 1, code: 'resource.agent.read', category: 'resource', name: '查看智能体', description: '查看资源', created_at: '' },
      { id: 3, code: 'directory.read', category: 'directory', name: '查看组织目录', description: '查看目录', created_at: '' },
    ]);
    expect(groups.map((group) => group.label)).toEqual(['智能体与应用', '组织与同步']);
    expect(groups[1].permissions.map((permission) => permission.code)).toEqual(['directory.read', 'directory.sync.manage']);
  });
});
