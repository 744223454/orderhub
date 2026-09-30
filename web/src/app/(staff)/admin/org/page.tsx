'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import type { TableProps, TreeDataNode } from 'antd';
import {
  Alert,
  Button,
  Card,
  Col,
  Input,
  Row,
  Segmented,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tree,
  Typography,
} from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { api, ApiError } from '@/lib/api';
import {
  buildOrgTree,
  collectOrgDeptIds,
  countDirectMembers,
  findOrgTreeNode,
  memberStatusMeta,
} from '@/lib/org';
import type { OrgTreeNode } from '@/lib/org';
import { ADMIN_ROLES } from '@/lib/roles';
import type { OrgDirectory, OrgMember } from '@/lib/types';

/**
 * 树的「全部成员」伪节点的 key。
 * 用非数字字符串：部门 id 都是数字，两者永不会撞 key。
 */
const ALL_KEY = 'all';

/**
 * 组织架构页。
 *
 * 权限与后端保持一致：接口挂在 RequireRole(user.RoleAdmin) 上（运营读整个通讯录
 * 没有业务理由），所以这里也包一层 AuthGuard，入口只在管理员导航里出现。
 */
export default function AdminOrgPage() {
  return (
    <AuthGuard allow={ADMIN_ROLES}>
      <OrgDirectoryView />
    </AuthGuard>
  );
}

function OrgDirectoryView() {
  // data 为 null 表示首屏还没加载完，loading 直接由它算出来。
  const [data, setData] = useState<OrgDirectory | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // 主动刷新与首屏加载分开：刷新时页面上已经有数据，只是要转个圈。
  const [refreshing, setRefreshing] = useState(false);

  const [selectedKey, setSelectedKey] = useState<string>(ALL_KEY);
  const [includeChildren, setIncludeChildren] = useState(true);
  const [keyword, setKeyword] = useState('');
  // 状态筛选的取值用字符串：'all' 与 '1' / '4' 这类状态码混在一组选项里，
  // 统一成字符串可以让 Segmented 的 value 类型干净，不必做联合类型体操。
  const [statusFilter, setStatusFilter] = useState<string>(ALL_KEY);

  const load = useCallback(async (force: boolean): Promise<void> => {
    try {
      // force 时保留旧数据：刷新失败不该把已经看到的组织架构清空，
      // 页面上给一条错误提示就够了。
      setData(await api.getOrgDirectory(force));
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '获取企业通讯录失败');
    } finally {
      setRefreshing(false);
    }
  }, []);

  // 首屏走缓存（不带 refresh）：每次打开页面都强制重抓，等于每开一次页面就烧几十次企微调用。
  // ⚠️ 这里会触发一条 react-hooks/set-state-in-effect 的 warn，是预期而非漏改，
  //    原因同其它取数页面（加载函数要复用，refresh 按钮也走它）。规则已降级为 warn。
  useEffect(() => {
    void load(false);
  }, [load]);

  /** 刷新按钮：绕过后端 5 分钟缓存重新抓取（可能要打几十次企微接口，几秒钟）。 */
  function handleRefresh(): void {
    setRefreshing(true);
    setLoadError(null);
    void load(true);
  }

  /**
   * 从快照派生出的全部视图数据。
   *
   * 刻意放在**一个** useMemo 里、依赖只有 data：`data?.departments ?? []` 这类
   * 逻辑表达式每次渲染都会产生新引用，若把它提到外面再当作下游 memo 的依赖，
   * 所有下游 memo 会每次渲染全部失效（exhaustive-deps 规则提示的正是这件事），
   * 白白重算一遍 72 人规模的过滤与组树。
   */
  const view = useMemo(() => {
    const departments = data?.departments ?? [];
    const members = data?.members ?? [];

    return {
      members,
      tree: buildOrgTree(departments, countDirectMembers(members)),
      /** 部门名查表，用于把部门 id 翻成可读名称。 */
      departmentNames: new Map(departments.map((item) => [item.id, item.name])),
      /** 成员名查表，用于把直属上级的 userid 翻成姓名。 */
      memberNames: new Map(members.map((item) => [item.userid, item.name])),
      /** 数据里**实际出现过**的状态，按取值排序，用于生成筛选项。 */
      statuses: [...new Set(members.map((item) => item.status))].sort((a, b) => a - b),
    };
  }, [data]);

  /**
   * 当前选中的部门节点。
   *
   * 找不到时返回 null（刷新后该部门消失、或数据还没到）—— 此时按「全部」处理，
   * 并且把树的 selectedKeys 也一起回落到「全部」，避免出现
   * 「左树没选中任何项、右表却只显示一部分人」这种对不上的状态。
   */
  const selectedNode = useMemo(
    () => (selectedKey === ALL_KEY ? null : findOrgTreeNode(view.tree, Number(selectedKey))),
    [view.tree, selectedKey],
  );
  const activeKey = selectedNode === null ? ALL_KEY : selectedKey;

  /** 当前应显示的成员。全部在前端算：成员总量是几十到几千，没必要再打一次接口。 */
  const rows = useMemo(() => {
    const scope =
      selectedNode === null
        ? null
        : includeChildren
          ? collectOrgDeptIds(selectedNode)
          : new Set([selectedNode.id]);

    const needle = keyword.trim().toLowerCase();
    const statusCode = statusFilter === ALL_KEY ? null : Number(statusFilter);

    return view.members.filter((member) => {
      if (scope !== null && !member.departments.some((item) => scope.has(item.id))) {
        return false;
      }
      if (statusCode !== null && member.status !== statusCode) {
        return false;
      }
      if (needle === '') {
        return true;
      }
      // 搜索范围：姓名 / 帐号 / 职务 / 部门名。部门名也搜是有意的——
      // 「研发部都有谁」既可以从左树点，也可以直接搜。
      return (
        member.name.toLowerCase().includes(needle) ||
        member.userid.toLowerCase().includes(needle) ||
        member.position.toLowerCase().includes(needle) ||
        member.departments.some((item) =>
          (view.departmentNames.get(item.id) ?? '').toLowerCase().includes(needle),
        )
      );
    });
  }, [view, selectedNode, includeChildren, keyword, statusFilter]);

  /** 状态筛选项按实际出现过的状态生成，不提供一行都筛不出来的选项。 */
  const statusOptions = useMemo(
    () => [
      { label: '全部状态', value: ALL_KEY },
      ...view.statuses.map((status) => ({
        label: memberStatusMeta(status).text,
        value: String(status),
      })),
    ],
    [view.statuses],
  );

  const treeData = useMemo<TreeDataNode[]>(() => {
    const toNode = (node: OrgTreeNode): TreeDataNode => ({
      key: String(node.id),
      title: (
        <span>
          {node.name}
          <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 6 }}>
            {node.directMemberCount} 人
          </Typography.Text>
        </span>
      ),
      children: node.children.map(toNode),
    });

    return [
      {
        key: ALL_KEY,
        title: `全部成员（${view.members.length} 人）`,
        children: view.tree.map(toNode),
      },
    ];
  }, [view]);

  const columns: TableProps<OrgMember>['columns'] = [
    {
      title: '姓名',
      dataIndex: 'name',
      width: 170,
      render: (_: unknown, member: OrgMember) => {
        const meta = memberStatusMeta(member.status);
        return (
          <Space size={4}>
            <span>{member.name}</span>
            <Tag color={meta.color}>{meta.text}</Tag>
          </Space>
        );
      },
    },
    {
      title: '帐号',
      dataIndex: 'userid',
      width: 170,
      render: (userid: string) => <Typography.Text code>{userid}</Typography.Text>,
    },
    {
      title: '职务',
      dataIndex: 'position',
      width: 130,
      render: (position: string) => position || '—',
    },
    {
      title: '所属部门',
      width: 260,
      render: (_: unknown, member: OrgMember) => (
        <Space size={4} wrap>
          {member.departments.map((item) => (
            <Tag
              key={item.id}
              // 主部门标蓝：多部门时一眼能看出「主」是哪个，与企微后台的口径一致。
              color={item.id === member.main_department_id ? 'blue' : undefined}
            >
              {view.departmentNames.get(item.id) ?? `#${item.id}`}
              {item.is_leader ? '（负责人）' : ''}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '直属上级',
      width: 150,
      render: (_: unknown, member: OrgMember) =>
        member.direct_leader_ids.length === 0
          ? '—'
          : // 上级本人可能不在可见范围内（查不到名字），此时回落显示 userid，别留空白。
            member.direct_leader_ids
              .map((userid) => view.memberNames.get(userid) ?? userid)
              .join('、'),
    },
  ];

  return (
    <Row gutter={16}>
      <Col xs={24} lg={9} xl={7}>
        <Card title="组织架构" size="small">
          {data === null ? (
            <Spin />
          ) : view.tree.length === 0 ? (
            // 空态必须给出**两种**可能：这不是防御性文案，而是真实的两种成因
            // （可见范围为空 / 接口形状变了），后端的解析层面区分不了它们。
            <Alert
              type="warning"
              showIcon
              title="没有读到任何部门"
              description="请确认企业微信自建应用的「可见范围」是否包含目标部门；若刚刚还能看到，也可能是接口返回结构发生了变化。"
            />
          ) : (
            <Tree
              treeData={treeData}
              selectedKeys={[activeKey]}
              showLine
              blockNode
              defaultExpandAll
              onSelect={(keys) => {
                // 再次点击已选中的节点会返回空数组，此时回落到「全部」，
                // 否则会出现「树上没选中、表里显示全部」的错位。
                setSelectedKey(keys.length > 0 ? String(keys[0]) : ALL_KEY);
              }}
            />
          )}
          <Typography.Paragraph
            type="secondary"
            style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}
          >
            数字为本部门「直属」成员数，不含子部门。只显示企业微信应用「可见范围」内的部门与成员。
          </Typography.Paragraph>
        </Card>
      </Col>

      <Col xs={24} lg={15} xl={17}>
        <Card
          title={`成员（${rows.length} / ${view.members.length}）`}
          size="small"
          extra={
            // 文案用「重新抓取」而不是「刷新」：这个按钮会 bypass 服务端缓存、真的重新
            // 调企微（约 1 + 部门数 次请求），名字要说清代价。
            // 顺带的好处：antd 会给**两个汉字**的按钮自动插空格（渲染成「刷 新」），
            // 三个字以上不会，按文案定位按钮的自动化脚本因此更稳。
            <Button size="small" onClick={handleRefresh} loading={refreshing}>
              重新抓取
            </Button>
          }
        >
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Space size={12} wrap>
              <Input
                allowClear
                value={keyword}
                placeholder="搜索姓名 / 帐号 / 职务 / 部门"
                style={{ width: 260 }}
                onChange={(e) => setKeyword(e.target.value)}
              />
              <Segmented
                value={statusFilter}
                options={statusOptions}
                onChange={(value) => setStatusFilter(String(value))}
              />
              <Space size={6}>
                <Switch
                  size="small"
                  checked={includeChildren}
                  disabled={activeKey === ALL_KEY}
                  onChange={setIncludeChildren}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  包含子部门
                </Typography.Text>
              </Space>
            </Space>

            {loadError !== null && <Alert type="error" title={loadError} showIcon />}

            <Table<OrgMember>
              rowKey="userid"
              size="small"
              columns={columns}
              dataSource={rows}
              loading={data === null}
              pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: true }}
              locale={{ emptyText: data === null ? ' ' : '没有符合条件的成员' }}
            />

            {data !== null && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                数据抓取于 {new Date(data.fetched_at).toLocaleString('zh-CN')}
                {data.from_cache ? '（服务端缓存，5 分钟内有效）' : '（刚抓取）'}
                ；企业微信接口按分钟限频， 请勿频繁点击刷新。
                <br />
                「未激活」表示该成员尚未激活企业微信，人在通讯录里、接口也返回，但无法登录企业微信。
              </Typography.Text>
            )}
          </Space>
        </Card>
      </Col>
    </Row>
  );
}
