import type { DirectoryDepartment } from './enterpriseApi';

export interface DepartmentTreeNode {
  title: string;
  value: number;
  key: number;
  department: DirectoryDepartment;
  children?: DepartmentTreeNode[];
}

function compareDepartments(a: DirectoryDepartment, b: DirectoryDepartment) {
  const order = (a.order_weight || '').localeCompare(b.order_weight || '', undefined, { numeric: true });
  return order || a.name.localeCompare(b.name, 'zh-CN') || a.id - b.id;
}

export function buildDepartmentTree(departments: DirectoryDepartment[]): DepartmentTreeNode[] {
  const sorted = [...departments].sort(compareDepartments);
  const nodes = new Map<number, DepartmentTreeNode>();
  sorted.forEach((department) => nodes.set(department.id, {
    title: department.name,
    value: department.id,
    key: department.id,
    department,
    children: [],
  }));

  const roots: DepartmentTreeNode[] = [];
  sorted.forEach((department) => {
    const node = nodes.get(department.id)!;
    const parent = department.parent_id ? nodes.get(department.parent_id) : undefined;
    if (parent && parent !== node) parent.children!.push(node);
    else roots.push(node);
  });

  const trim = (node: DepartmentTreeNode): DepartmentTreeNode => (
    node.children?.length ? { ...node, children: node.children.map(trim) } : { ...node, children: undefined }
  );
  return roots.map(trim);
}

export interface DepartmentTreeRow {
  department: DirectoryDepartment;
  depth: number;
  hasChildren: boolean;
}

export function flattenDepartmentTree(
  departments: DirectoryDepartment[],
  collapsedIds: Set<number> = new Set(),
): DepartmentTreeRow[] {
  const rows: DepartmentTreeRow[] = [];
  const visit = (nodes: DepartmentTreeNode[], depth: number) => {
    nodes.forEach((node) => {
      rows.push({ department: node.department, depth, hasChildren: Boolean(node.children?.length) });
      if (node.children?.length && !collapsedIds.has(node.value)) visit(node.children, depth + 1);
    });
  };
  visit(buildDepartmentTree(departments), 0);
  return rows;
}
