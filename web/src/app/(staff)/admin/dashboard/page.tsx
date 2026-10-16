'use client';

import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Col, Row, Space, Statistic } from 'antd';

import { PageContainer } from '@/components/page-container';
import { api, ApiError } from '@/lib/api';
import { formatAmount } from '@/lib/format';
import type { OrderStats } from '@/lib/types';

import { DailyChart } from './_components/daily-chart';
import { StatusChart } from './_components/status-chart';

/**
 * 数据看板页（运营 / 管理员可见）。
 *
 * 数据来自 GET /api/admin/orders/stats，三个区块：
 * 汇总卡片、状态分布环形图、近 14 天趋势折线图。
 *
 * 取数沿用「我的订单」页的模式（load 函数 + useCallback + effect），
 * 首屏用 null 表示「还没加载完」——loading 由它算出，不额外维护 boolean。
 * 401 跳转由路由守卫负责，页面自身不处理。
 */
export default function DashboardPage() {
  const [stats, setStats] = useState<OrderStats | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  /** 拉取订单统计。首屏加载与失败重试共用同一入口。 */
  const loadStats = useCallback(async (): Promise<void> => {
    try {
      setStats(await api.getOrderStats());
      setLoadError(null);
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : '加载统计数据失败');
    }
  }, []);

  // 预期触发一条 react-hooks/set-state-in-effect 的 warn——项目已知项
  // （详见「我的订单」页里同类注释），不是漏改。
  useEffect(() => {
    void loadStats();
  }, [loadStats]);

  return (
    <PageContainer title="数据看板">
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        {stats === null ? (
          loadError === null ? (
            // 首屏加载态。loading 由「stats 为 null」算出，不额外维护 boolean。
            <Card loading />
          ) : (
            // 首屏失败态：给重试入口，而不是让卡片一直转圈——转圈会把失败伪装成
            // 「还在加载」。做法与组织架构页的失败态一致；loadError 只可能出现在
            // 首屏（成功后没有任何路径会再触发加载），所以成功分支无需错误条。
            <Alert
              type="error"
              showIcon
              title="加载失败"
              description={loadError}
              action={
                <Button
                  size="small"
                  onClick={() => {
                    setLoadError(null);
                    void loadStats();
                  }}
                >
                  重新加载
                </Button>
              }
            />
          )
        ) : (
          <>
            <Row gutter={16}>
              <Col xs={24} sm={12} lg={8}>
                <Card>
                  <Statistic title="总订单数" value={stats.total_orders} />
                </Card>
              </Col>
              <Col xs={24} sm={12} lg={8}>
                <Card>
                  {/* 金额单位是「分」，展示走 formatAmount（全程整数运算，别手写 /100）。 */}
                  <Statistic title="总金额" value={formatAmount(stats.total_amount)} />
                </Card>
              </Col>
              <Col xs={24} sm={12} lg={8}>
                {/*
                  今日订单：取 daily 末条的 count——契约保证 daily 恒 14 条、按日期升序、
                  末条为「北京时间今天」；今天尚无订单时显示 0，属正常口径。
                */}
                <Card>
                  <Statistic title="今日订单" value={stats.daily[stats.daily.length - 1].count} />
                </Card>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col xs={24} lg={10}>
                <Card title="订单状态分布">
                  <StatusChart data={stats.by_status} />
                </Card>
              </Col>
              <Col xs={24} lg={14}>
                <Card title="近 14 天订单趋势">
                  <DailyChart data={stats.daily} />
                </Card>
              </Col>
            </Row>
          </>
        )}
      </Space>
    </PageContainer>
  );
}
