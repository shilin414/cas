import { describe, expect, it } from 'vitest';
import { mergeDepartmentGrants } from '../AccessGroupsPage';

describe('mergeDepartmentGrants', () => {
  it('preserves include_children=false while editing an existing local group', () => {
    const result = mergeDepartmentGrants([3], [{ department_id: 3, name: '财务', include_children: false, covered_users: 8 }], [{ id: 3, name: '财务', open_department_id: 'od-3', parent_open_department_id: '0', order_weight: '', is_active: true }]);
    expect(result).toEqual([{ department_id: 3, name: '财务', include_children: false, covered_users: 8 }]);
  });
});
