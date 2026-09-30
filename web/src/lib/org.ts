/**
 * 组织架构页的纯函数：成员状态映射，以及「扁平部门 → 部门树」的组装。
 *
 * 树为什么放在前端组装：
 *   后端返回扁平的 departments / members，是刻意的 —— 这一页无论怎么设计，都要同时
 *   持有两份视图：左树的层次结构，和右表的**跨部门**成员集合（搜索、筛选都要跨部门）。
 *   后端若直接给树，搜索时前端还得把它拍平成成员列表，等于多绕一道；
 *   而展示形态（树、折叠面板、图形化组织图）随时可能变，接口不该跟着变。
 *
 * 这个文件不依赖 React 与 antd，可以单独跑（纯函数，便于验证边界）。
 */

import type { OrgDepartment, OrgMember, OrgMemberStatus } from './types';

/** 部门树的节点。只装数据，标题等展示内容由页面渲染。 */
export interface OrgTreeNode {
  /** 部门 id。 */
  id: number;
  name: string;
  /** 本部门**直属**成员数（不含子部门）。 */
  directMemberCount: number;
  /** 本部门负责人（部门维度的 department_leader）。 */
  leaderUserIDs: string[];
  children: OrgTreeNode[];
}

/** 成员状态的展示元数据。颜色取值必须是 antd Tag 认识的，写错会静默回落成默认色。 */
const MEMBER_STATUS_META: Record<OrgMemberStatus, { text: string; color: string }> = {
  1: { text: '已激活', color: 'success' },
  2: { text: '已禁用', color: 'error' },
  4: { text: '未激活', color: 'warning' },
  5: { text: '已退出', color: 'default' },
};

/**
 * 取成员状态的展示元数据。
 *
 * 未知取值回落成「未知(n)」而不是直接下标取值：企微新增状态值时，
 * 页面应该仍能渲染，而不是读 undefined 的属性抛错、整张表连带左树一起白屏
 * （OrderStatusTag 当初就踩过这个坑）。
 */
export function memberStatusMeta(status: number): { text: string; color: string } {
  const meta = MEMBER_STATUS_META[status as OrgMemberStatus];
  return meta ?? { text: `未知(${status})`, color: 'default' };
}

/**
 * 统计每个部门的**直属**成员数。
 *
 * 「直属」= 成员的 departments 里包含该部门，与他是否把这里设为主部门无关；
 * 一个成员属于多个部门时，每个部门各算一个 —— 这正是通讯录里的真实关系。
 */
export function countDirectMembers(members: OrgMember[]): Map<number, number> {
  const counts = new Map<number, number>();
  for (const member of members) {
    for (const department of member.departments) {
      counts.set(department.id, (counts.get(department.id) ?? 0) + 1);
    }
  }
  return counts;
}

/**
 * 把扁平部门列表组装成树。
 *
 * 必须兜住的三个边界（前两个在真实环境里一定会出现）：
 *   1. **孤儿**：parent_id 指向的部门不在列表里。自建应用只能读「可见范围」内的通讯录，
 *      父部门不在范围内时就会出现；若按「找不到父亲就丢弃」处理，会造成**整棵子树消失**，
 *      而且页面上看不出任何异常。
 *   2. **环**：数据异常时父子互指。递归组装会直接爆栈。
 *   3. **重复 id**：同一 id 出现两次会生成两个 key 相同的节点，React 报 duplicate key。
 *
 * 处理方式是「能挂上去就挂，挂不上一律提升到根层」：层次显示上可能略有失真，
 * 但绝不凭空少掉部门 —— 少一个部门比多一层嵌套难发现得多。
 *
 * 复杂度 O(n)；递归深度等于树的层数（企微上限 15 层），不会栈溢出。
 */
export function buildOrgTree(
  departments: OrgDepartment[],
  directCounts: Map<number, number>,
): OrgTreeNode[] {
  // 先按 (order, id) 排序并去重：排序让树不依赖入参顺序，去重挡住重复 id。
  const ordered: OrgDepartment[] = [];
  const seen = new Set<number>();
  for (const department of [...departments].sort(compareDepartments)) {
    if (seen.has(department.id)) continue;
    seen.add(department.id);
    ordered.push(department);
  }

  const byId = new Map<number, OrgDepartment>(ordered.map((item) => [item.id, item]));
  const childrenOf = new Map<number, OrgDepartment[]>();
  for (const department of ordered) {
    const siblings = childrenOf.get(department.parent_id);
    if (siblings === undefined) {
      childrenOf.set(department.parent_id, [department]);
    } else {
      siblings.push(department);
    }
  }

  // placed 同时承担两个职责：环的出口（访问过就不再进入）与重复挂载的兜底。
  const placed = new Set<number>();

  const attach = (department: OrgDepartment, siblings: OrgTreeNode[]): void => {
    if (placed.has(department.id)) return;
    placed.add(department.id);

    const node: OrgTreeNode = {
      id: department.id,
      name: department.name,
      directMemberCount: directCounts.get(department.id) ?? 0,
      leaderUserIDs: department.leader_user_ids,
      children: [],
    };
    siblings.push(node);

    for (const child of childrenOf.get(department.id) ?? []) {
      attach(child, node.children);
    }
  };

  const roots: OrgTreeNode[] = [];
  for (const department of ordered) {
    // 根有两种：parent_id = 0 的，以及父亲不在列表里的孤儿。
    if (department.parent_id === 0 || !byId.has(department.parent_id)) {
      attach(department, roots);
    }
  }
  // 剩下的是环里的节点（互相指，谁都到不了根）。一并提升到根层，宁可扁平也不隐藏。
  for (const department of ordered) {
    if (!placed.has(department.id)) {
      attach(department, roots);
    }
  }

  return roots;
}

/**
 * 在部门树里按 id 找节点，找不到返回 null。
 * 用迭代而不是递归：调用方可能拿它处理任意深度的树，不必受层数假设约束。
 */
export function findOrgTreeNode(nodes: OrgTreeNode[], id: number): OrgTreeNode | null {
  const stack = [...nodes];
  while (stack.length > 0) {
    const node = stack.pop() as OrgTreeNode;
    if (node.id === id) return node;
    stack.push(...node.children);
  }
  return null;
}

/** 收集某节点自身及全部后代的部门 id，用于「包含子部门」筛选。 */
export function collectOrgDeptIds(node: OrgTreeNode): Set<number> {
  const ids = new Set<number>();
  const stack = [node];
  while (stack.length > 0) {
    const current = stack.pop() as OrgTreeNode;
    ids.add(current.id);
    stack.push(...current.children);
  }
  return ids;
}

/** 部门的排序规则：先按企微给的 order，再按 id（order 相同的情况真实存在）。 */
function compareDepartments(left: OrgDepartment, right: OrgDepartment): number {
  if (left.order !== right.order) {
    return left.order - right.order;
  }
  return left.id - right.id;
}
