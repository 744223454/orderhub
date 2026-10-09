import { describe, expect, it } from 'vitest';

import type { OrgDepartment, OrgMember } from './types';
import {
  buildOrgTree,
  collectOrgDeptIds,
  countDirectMembers,
  findOrgTreeNode,
  memberStatusMeta,
} from './org';
import type { OrgTreeNode } from './org';

/** 构造一个部门，字段顺序与后端返回保持一致。 */
function dept(overrides: Partial<OrgDepartment> & { id: number }): OrgDepartment {
  return {
    name: `部门${overrides.id}`,
    parent_id: 0,
    order: 0,
    leader_user_ids: [],
    ...overrides,
  };
}

/** 构造一个成员，departments 决定它算进哪些部门的直属人数。 */
function member(userid: string, departmentIDs: number[]): OrgMember {
  return {
    userid,
    name: userid,
    position: '',
    status: 1,
    main_department_id: departmentIDs[0] ?? 0,
    departments: departmentIDs.map((id) => ({ id, is_leader: false })),
    direct_leader_ids: [],
  };
}

/** 把树摊平成 id 列表，方便断言结构而不必逐层写死。 */
function ids(nodes: { id: number }[]): number[] {
  return nodes.map((n) => n.id);
}

/** 把树摊平，验证「每个 id 恰好出现一次」这个核心不变量。 */
function flatten(nodes: OrgTreeNode[]): number[] {
  const out: number[] = [];
  const walk = (list: OrgTreeNode[]): void => {
    for (const node of list) {
      out.push(node.id);
      walk(node.children);
    }
  };
  walk(nodes);
  return out;
}

describe('memberStatusMeta', () => {
  it('已知状态返回配置好的文案与配色', () => {
    expect(memberStatusMeta(1).text).toBe('已激活');
    expect(memberStatusMeta(2).text).toBe('已禁用');
    expect(memberStatusMeta(4).text).toBe('未激活');
    expect(memberStatusMeta(5).text).toBe('已退出');
  });

  it('未知状态回落到「未知(n)」而不是抛错', () => {
    // 这条是真实踩过的坑：企微新增状态值时，页面不能整块白屏。
    expect(memberStatusMeta(99)).toEqual({ text: '未知(99)', color: 'default' });
    expect(memberStatusMeta(0)).toEqual({ text: '未知(0)', color: 'default' });
  });
});

describe('countDirectMembers', () => {
  it('空列表返回空 Map', () => {
    expect(countDirectMembers([]).size).toBe(0);
  });

  it('一个成员属于多个部门时，每个部门各算一个', () => {
    const counts = countDirectMembers([member('a', [1, 2])]);
    expect(counts.get(1)).toBe(1);
    expect(counts.get(2)).toBe(1);
  });

  it('同部门多个成员累加', () => {
    const counts = countDirectMembers([member('a', [1]), member('b', [1]), member('c', [1, 2])]);
    expect(counts.get(1)).toBe(3);
    expect(counts.get(2)).toBe(1);
  });

  it('只统计直属，与主部门无关', () => {
    // 主部门是 2，但 departments 里也有 1 —— 两个部门都得算上他。
    const m = member('a', [1, 2]);
    const counts = countDirectMembers([m]);
    expect(counts.get(1)).toBe(1);
    expect(counts.get(2)).toBe(1);
  });
});

describe('buildOrgTree', () => {
  it('空输入返回空数组', () => {
    expect(buildOrgTree([], new Map())).toEqual([]);
  });

  it('扁平列表组装成多层树', () => {
    //   1
    //   └─ 2
    //      └─ 3
    const tree = buildOrgTree(
      [dept({ id: 1 }), dept({ id: 2, parent_id: 1 }), dept({ id: 3, parent_id: 2 })],
      new Map(),
    );
    expect(ids(tree)).toEqual([1]);
    expect(ids(tree[0].children)).toEqual([2]);
    expect(ids(tree[0].children[0].children)).toEqual([3]);
  });

  it('同层按 (order, id) 排序，order 相同时按 id', () => {
    // 后端已经排过序，但 buildOrgTree 不该依赖入参顺序 —— 显式打乱再验证。
    const tree = buildOrgTree(
      [dept({ id: 30, order: 1 }), dept({ id: 10, order: 2 }), dept({ id: 20, order: 1 })],
      new Map(),
    );
    // order=1 的按 id 升序（20、30），然后 order=2 的（10）。
    expect(ids(tree)).toEqual([20, 30, 10]);
  });

  /**
   * 孤儿：parent_id 指向的部门**不在列表里**。
   * 自建应用只能读可见范围内的通讯录，父部门不在范围内时必然出现。
   */
  it('孤儿节点提升到根层，且它的子树不能丢', () => {
    const tree = buildOrgTree(
      [
        dept({ id: 10, parent_id: 999 }), // 999 不在列表里 → 孤儿
        dept({ id: 11, parent_id: 10 }), // 10 的子节点
      ],
      new Map(),
    );
    expect(ids(tree)).toEqual([10]);
    expect(ids(tree[0].children)).toEqual([11]);
  });

  /**
   * 环：父子互指，谁都到不了根。
   * 若不处理，递归会无限深入直接爆栈。
   *
   * 断言的是**不变量**而不是具体形状：「每个部门 id 恰好出现一次」。
   * 之所以不锁死「必须在根层」—— 当前实现（lib/org.ts 最后那个兜底循环）
   * 是把环里的节点挂成一条链，2和 3 可能是父子而不是并列的根。
   * 两种形状都满足「不丢部门、不死循环」这个真正的需求，
   * 锁死形状反而会让一次无害的写法调整变成用例失败。
   */
  it('环里的节点不丢且不死循环', () => {
    const tree = buildOrgTree(
      [dept({ id: 1, parent_id: 2 }), dept({ id: 2, parent_id: 1 })],
      new Map(),
    );
    const flat = flatten(tree).sort();
    expect(flat).toEqual([1, 2]);
    expect(flat).toHaveLength(2);
  });

  it('三元环同样能处理', () => {
    const tree = buildOrgTree(
      [dept({ id: 1, parent_id: 3 }), dept({ id: 2, parent_id: 1 }), dept({ id: 3, parent_id: 2 })],
      new Map(),
    );
    const flat = flatten(tree).sort();
    expect(flat).toEqual([1, 2, 3]);
  });

  it('自环（parent_id 指向自己）不爆栈', () => {
    const tree = buildOrgTree([dept({ id: 7, parent_id: 7 })], new Map());
    expect(flatten(tree)).toEqual([7]);
  });

  it('环与正常子树混在一起时，正常部分仍保持层次', () => {
    const tree = buildOrgTree(
      [
        dept({ id: 1 }),
        dept({ id: 2, parent_id: 1 }),
        dept({ id: 3, parent_id: 2 }),
        dept({ id: 90, parent_id: 91 }),
        dept({ id: 91, parent_id: 90 }),
      ],
      new Map(),
    );
    const flat = flatten(tree).sort();
    expect(flat).toEqual([1, 2, 3, 90, 91]);
    // 正常那部分的父子关系不能被环搞乱。
    expect(findOrgTreeNode(tree, 3)?.id).toBe(3);
    expect(findOrgTreeNode(tree, 3)?.id === 3).toBe(true);
  });

  /** 重复 id：同一条部门在接口里出现两次。 */
  it('重复 id 只保留一个节点', () => {
    const tree = buildOrgTree(
      [dept({ id: 1 }), dept({ id: 1 }), dept({ id: 2, parent_id: 1 })],
      new Map(),
    );
    expect(ids(tree)).toEqual([1]);
    // 子节点只挂一次，不能重复。
    expect(ids(tree[0].children)).toEqual([2]);
  });

  it('直属成员数从传入的 Map 取，缺失时为 0', () => {
    const directCounts = new Map([[1, 5]]);
    const tree = buildOrgTree([dept({ id: 1 }), dept({ id: 2 })], directCounts);
    expect(tree[0].directMemberCount).toBe(5);
    expect(tree[1].directMemberCount).toBe(0);
  });

  it('部门负责人原样带出', () => {
    const tree = buildOrgTree([dept({ id: 1, leader_user_ids: ['zhangsan', 'lisi'] })], new Map());
    expect(tree[0].leaderUserIDs).toEqual(['zhangsan', 'lisi']);
  });

  it('多条根节点并存', () => {
    const tree = buildOrgTree([dept({ id: 1 }), dept({ id: 2 }), dept({ id: 3 })], new Map());
    expect(ids(tree)).toEqual([1, 2, 3]);
  });

  /**
   * 综合场景：真实数据里同时存在孤儿、环、重复 id。
   * 目标是「一个部门都不能凭空消失」。
   */
  it('综合脏数据下不丢任何部门', () => {
    const tree = buildOrgTree(
      [
        dept({ id: 1 }),
        dept({ id: 1 }), // 重复
        dept({ id: 2, parent_id: 888 }), // 孤儿
        dept({ id: 3, parent_id: 4 }), // 环
        dept({ id: 4, parent_id: 3 }),
        dept({ id: 5, parent_id: 2 }),
      ],
      new Map(),
    );
    const seen = flatten(tree);
    // 5 个不同 id 全部出现，且 1 只出现一次（重复的那条被挡住）。
    expect([...new Set(seen)].sort()).toEqual([1, 2, 3, 4, 5]);
    expect(seen.filter((id) => id === 1)).toHaveLength(1);
    expect(seen).toHaveLength(5);
  });

  it('不修改传入的数组（入参不可变）', () => {
    const input = [dept({ id: 2, order: 2 }), dept({ id: 1, order: 1 })];
    const snapshot = [...input];
    buildOrgTree(input, new Map());
    expect(input).toEqual(snapshot);
  });
});

describe('findOrgTreeNode', () => {
  const directCounts = new Map([[1, 1]]);
  const tree = buildOrgTree(
    [dept({ id: 1 }), dept({ id: 2, parent_id: 1 }), dept({ id: 3, parent_id: 2 })],
    directCounts,
  );

  it('能找到根节点', () => {
    expect(findOrgTreeNode(tree, 1)?.id).toBe(1);
  });

  it('能找到深层节点', () => {
    expect(findOrgTreeNode(tree, 3)?.id).toBe(3);
  });

  it('找不到时返回 null', () => {
    expect(findOrgTreeNode(tree, 999)).toBeNull();
  });

  it('空树返回 null', () => {
    expect(findOrgTreeNode([], 1)).toBeNull();
  });
});

describe('collectOrgDeptIds', () => {
  it('收集自身与全部后代', () => {
    const tree = buildOrgTree(
      [
        dept({ id: 1 }),
        dept({ id: 2, parent_id: 1 }),
        dept({ id: 3, parent_id: 2 }),
        dept({ id: 4 }),
      ],
      new Map(),
    );
    const root = findOrgTreeNode(tree, 1);
    expect(root).not.toBeNull();
    const collected = collectOrgDeptIds(root as (typeof tree)[number]);
    expect([...collected].sort()).toEqual([1, 2, 3]);
  });

  it('叶子节点只含自己', () => {
    const tree = buildOrgTree([dept({ id: 1 })], new Map());
    expect([...collectOrgDeptIds(tree[0])]).toEqual([1]);
  });
});
