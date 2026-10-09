'use client';

import { useCallback, useEffect, useState } from 'react';
import type { TableProps } from 'antd';
import {
  Alert,
  Button,
  Card,
  Dropdown,
  Input,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';

import { AuthGuard } from '@/components/auth-guard';
import { UserCreateModal } from '@/components/user-create-modal';
import { api, ApiError } from '@/lib/api';
import { useSession } from '@/lib/auth';
import { identityProviderLabel } from '@/lib/identity';
import { ADMIN_ROLES, ASSIGNABLE_ROLES, ROLE_LABELS } from '@/lib/roles';
import type { AdminUser, AdminUserPage, IdentityProvider, Role } from '@/lib/types';

/** 每页条数。后端上限是 100，这里固定一个对表格友好的值。 */
const PAGE_SIZE = 20;

/**
 * 角色标签的配色。
 * 取值必须是 antd Tag 认识的那几个（状态色 5 个 + 命名预设色 13 个），写错会静默回落成默认色。
 */
const ROLE_COLORS: Record<Role, string> = {
  admin: 'red',
  ops: 'blue',
  user: 'default',
};

/**
 * 用户管理页。
 *
 * 为什么要额外包一层 AuthGuard：外层 (staff)/layout 放行的是 admin + ops，
 * 而用户管理是权限边界本身 —— 后端挂的是 RequireRole(user.RoleAdmin)。
 * 前端入口的显隐必须与后端一致，否则运营能看到页面、点下去全是 403。
 */
export default function AdminUsersPage() {
  return (
    <AuthGuard allow={ADMIN_ROLES}>
      <UsersTable />
    </AuthGuard>
  );
}

function UsersTable() {
  const { session } = useSession();

  // data 为 null 表示首屏还没加载完，loading 直接由它算出来，不必再维护一个 boolean。
  const [data, setData] = useState<AdminUserPage | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const [page, setPage] = useState(1);
  // keyword 是输入框里的值，appliedKeyword 是真正发出去查询的值：两者分开，
  // 才不会出现「每敲一个字就打一次接口」或者「敲了没回车却也触发了查询」。
  const [keyword, setKeyword] = useState('');
  const [appliedKeyword, setAppliedKeyword] = useState('');

  // 行内操作的状态与错误，与整页加载分开 —— 否则点一次改角色整张表都会变成 loading。
  const [actingId, setActingId] = useState<number | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // 「新建用户」弹窗的开合状态。放在页面而不是弹窗内部：触发按钮在页面上，两处要联动。
  const [createOpen, setCreateOpen] = useState(false);

  const loadUsers = useCallback(async (): Promise<void> => {
    try {
      setData(await api.listUsers({ page, pageSize: PAGE_SIZE, keyword: appliedKeyword }));
      // 成功时顺手清掉上次的错误，否则重试成功之后旧的红色横幅还挂在页面上。
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '加载用户列表失败');
    }
  }, [page, appliedKeyword]);

  // 首屏加载、翻页、换关键字都走同一个入口。
  // ⚠️ 这里会触发一条 react-hooks/set-state-in-effect 的 warn，是预期而非漏改，
  //    原因与 (user)/orders 页相同：取数函数要复用（改完角色后可能重拉），
  //    为消一条警告把逻辑复制一遍不划算。规则已降级为 warn。
  useEffect(() => {
    void loadUsers();
  }, [loadUsers]);

  /**
   * 修改某个用户的角色。
   *
   * 成功后就地替换那一行，而不是整表重拉：后端返回的就是这次操作的权威值。
   * ⚠️ 别写成追加 —— rowKey="id" 撞 key，React 会报 duplicate key。
   */
  async function handleRoleChange(target: AdminUser, role: Role): Promise<void> {
    if (target.role === role) return;

    setActingId(target.id);
    setActionError(null);
    setNotice(null);
    try {
      const updated = await api.updateUserRole(target.id, { role });
      setData((prev) =>
        prev === null
          ? null
          : { ...prev, items: prev.items.map((u) => (u.id === updated.id ? updated : u)) },
      );

      // 改的是自己时要说一声：JWT 里编码的角色是签发那一刻的快照，
      // 不重新登录的话，界面上会继续按旧角色显示，而接口已经按新角色判权限了。
      setNotice(
        session !== null && session.user.id === updated.id
          ? '已修改你自己的角色，请重新登录后生效'
          : null,
      );
    } catch (e) {
      setActionError(e instanceof ApiError ? e.message : '修改角色失败，请稍后重试');
    } finally {
      setActingId(null);
    }
  }

  /**
   * 解绑某个用户的外部身份。
   *
   * 解绑改变了这个账号的「登录方式」这一整列，所以这里选择整页重拉，
   * 而不是像改角色那样就地替换。
   */
  async function handleUnbind(target: AdminUser, provider: IdentityProvider): Promise<void> {
    setActingId(target.id);
    setActionError(null);
    setNotice(null);
    try {
      await api.unbindUserIdentity(target.id, provider);
      await loadUsers();
    } catch (e) {
      setActionError(e instanceof ApiError ? e.message : '解绑失败，请稍后重试');
    } finally {
      setActingId(null);
    }
  }

  const columns: TableProps<AdminUser>['columns'] = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: '用户名', dataIndex: 'name' },
    {
      title: '角色',
      dataIndex: 'role',
      width: 110,
      render: (role: Role) => <Tag color={ROLE_COLORS[role]}>{ROLE_LABELS[role] ?? role}</Tag>,
    },
    {
      title: '登录方式',
      width: 260,
      render: (_: unknown, u: AdminUser) => (
        <Space size={4} wrap>
          {/* 「无密码」是给管理员看的警示：企业微信扫码自动建出的账号就用不了密码登录，
              若它同时是管理员，一旦企微那条路走不通就没人能进管理端了。 */}
          {u.has_password ? <Tag>密码</Tag> : <Tag color="warning">无密码</Tag>}
          {u.identities.map((identity) => (
            <Tag key={identity.id} color="cyan">
              {identityProviderLabel(identity.provider)}：{identity.external_id}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 180,
      render: (createdAt: string) => new Date(createdAt).toLocaleString('zh-CN'),
    },
    {
      title: '操作',
      width: 240,
      render: (_: unknown, u: AdminUser) => (
        <Space size={4} wrap>
          <Dropdown
            trigger={['click']}
            menu={{
              items: ASSIGNABLE_ROLES.map((role) => ({
                key: role,
                label: `设为${ROLE_LABELS[role] ?? role}`,
                // 当前角色置灰：既说明「现在是什么」，也省掉一次无意义的请求。
                disabled: role === u.role,
              })),
              onClick: ({ key }) => void handleRoleChange(u, key as Role),
            }}
          >
            <Button type="link" style={{ padding: 0 }} loading={actingId === u.id}>
              改角色
            </Button>
          </Dropdown>

          {u.identities.map((identity) => (
            <Popconfirm
              key={identity.id}
              title={`解绑${identityProviderLabel(identity.provider)}？`}
              description="解绑后该账号无法再用这种方式登录，下次用它扫码会被当成新用户重新建号。"
              okText="确认解绑"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={() => void handleUnbind(u, identity.provider)}
            >
              <Button type="link" danger style={{ padding: 0 }}>
                解绑{identityProviderLabel(identity.provider)}
              </Button>
            </Popconfirm>
          ))}
        </Space>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title="用户管理"
        extra={
          <Button type="primary" onClick={() => setCreateOpen(true)}>
            新建用户
          </Button>
        }
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Input.Search
            allowClear
            value={keyword}
            placeholder="按用户名搜索；输入数字则按 ID 精确查"
            style={{ maxWidth: 360 }}
            onChange={(e) => setKeyword(e.target.value)}
            onSearch={(value) => {
              // 换关键字必须回到第一页：停在第 3 页去搜一个只有 1 条结果的词，会直接看到空表。
              setPage(1);
              setAppliedKeyword(value.trim());
            }}
          />

          {loadError !== null && <Alert type="error" title={loadError} showIcon />}
          {actionError !== null && <Alert type="error" title={actionError} showIcon />}
          {notice !== null && (
            <Alert type="info" title={notice} showIcon closable onClose={() => setNotice(null)} />
          )}

          <Table<AdminUser>
            rowKey="id"
            columns={columns}
            // data 为 null 时给空数组，Table 才不会因为 dataSource 是 null 而崩。
            dataSource={data?.items ?? []}
            loading={data === null}
            // 分页状态以后端回显为准：入参越界时后端会回落，前端跟着它走就不会出现
            // 「页码显示第 9 页、数据其实是第 1 页」这种错位。
            pagination={{
              current: data?.page ?? 1,
              pageSize: data?.page_size ?? PAGE_SIZE,
              total: data?.total ?? 0,
              showSizeChanger: false,
            }}
            onChange={(pagination) => setPage(pagination.current ?? 1)}
          />

          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            标注「无密码」的账号只能用企业微信扫码登录。请始终保留至少一个既有密码、
            又能进管理端的账号，作为企业微信那条路走不通时的后路。
          </Typography.Text>
        </Space>
      </Card>

      <UserCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={() => {
          // 新账号必然落在第 1 页（列表按 created_at DESC）：回第 1 页重拉，由后端决定顺序与 total。
          // ⚠️ 已在第 1 页时 setPage(1) 不触发变更、effect 不会重跑，必须显式补一次。
          setPage(1);
          if (page === 1) {
            void loadUsers();
          }
        }}
      />
    </Space>
  );
}
