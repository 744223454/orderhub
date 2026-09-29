'use client';

import { Card, Typography } from 'antd';

/**
 * 订单管理页（运营 / 管理员）。
 *
 * 后端 /api/admin/* 全部要求 admin 或 ops 角色，
 * 角色不符时后端返回 403，前端由 (staff)/layout.tsx 的守卫提前拦下。
 */
export default function AdminOrdersPage() {
  return (
    <Card title="订单管理">
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        TODO: 由你实现。用 api.listAllOrders() 拉全量订单，Table 展示；
        再按行内状态给出当前可执行的操作—— paid → 发货(api.shipOrder) / 退款(api.refundOrder)，
        shipped → 完成(api.completeOrder) / 退款(api.refundOrder)， pending
        与已结束的状态则不给按钮。 这些规则和后端状态机一致，判定逻辑别在前端重写一遍，
        写错了只会让用户点了才发现 409。
      </Typography.Paragraph>
    </Card>
  );
}
