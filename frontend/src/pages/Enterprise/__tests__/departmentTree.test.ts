import { describe, expect, it } from 'vitest';
import { buildDepartmentTree, flattenDepartmentTree } from '../departmentTree';
import type { DirectoryDepartment } from '../enterpriseApi';

const departments: DirectoryDepartment[] = [
  { id: 3, name: '二级部门', parent_id: 2, open_department_id: 'od-3', parent_open_department_id: 'od-2', order_weight: '1', is_active: true },
  { id: 1, name: '另一根部门', parent_id: null, open_department_id: 'od-1', parent_open_department_id: '0', order_weight: '2', is_active: true },
  { id: 2, name: '一级部门', parent_id: null, open_department_id: 'od-2', parent_open_department_id: '0', order_weight: '1', is_active: true },
];

describe('departmentTree', () => {
  it('nests child departments under their real parent', () => {
    const tree = buildDepartmentTree(departments);
    expect(tree.map((node) => node.value)).toEqual([2, 1]);
    expect(tree[0].children?.map((node) => node.value)).toEqual([3]);
  });

  it('flattens in hierarchy order with depth metadata', () => {
    expect(flattenDepartmentTree(departments).map((row) => [row.department.id, row.depth])).toEqual([[2, 0], [3, 1], [1, 0]]);
  });
});
