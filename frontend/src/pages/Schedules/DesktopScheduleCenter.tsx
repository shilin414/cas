/**
 * DesktopScheduleCenter — 桌面自动化列表（Architecture 2.0 §53，从
 * SchedulesPage.tsx 拆出）。
 *
 * 详情 Drawer / 编辑 Modal 由路由驱动（/schedules/:id、/schedules/:id/edit、
 * /schedules/new）—— Desktop 列表只负责列表本身与跳转。
 */
import React from 'react';
import {
  Alert,
  Button,
  Input,
  Popconfirm,
  Segmented,
  Space,
  Switch,
  Table,
} from 'antd';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useSchedules } from '@/hooks/useSchedules';
import { ScheduleStatusTag } from '@/components/Schedules/ScheduleStatusTag';
import { EmptyState, LoadingState, PageHeader, PageSurface } from '@/components/ProductUI';
import { describeSchedulePlan, formatDateTime } from '@/lib/scheduleFormat';
import type { Schedule, ScheduleStatusFilter } from '@/types/schedule';
import './SchedulesPage.css';

export function DesktopScheduleCenter() {
  const navigate = useNavigate();
  const {
    data, loading, loadingMore, error, errorPhase, hasMore, loadMore,
    status, search, isMutating,
    setStatus, setSearch, reload, toggleEnabled, runNow, remove,
  } = useSchedules();

  // 错误分类（三次复审 §30–§31）：fatal = 第一页就失败且没有任何数据；
  // partial = 已有数据时刷新失败 → 警告 + 旧数据继续展示。
  const fatalError = Boolean(error) && data.length === 0;
  const partialError = Boolean(error) && data.length > 0
    && errorPhase !== 'loadMore';

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (_: unknown, s: Schedule) => (
        <div>
          <div className="schedule-card-name">{s.name}</div>
          <span className="schedule-detail-meta">{s.prompt.slice(0, 40)}{s.prompt.length > 40 ? '…' : ''}</span>
        </div>
      ),
    },
    {
      title: '执行计划',
      key: 'plan',
      render: (_: unknown, s: Schedule) => describeSchedulePlan(s),
    },
    {
      title: '下次执行',
      dataIndex: 'next_run_at',
      key: 'next_run_at',
      render: (v: string | null) => (
        <time dateTime={v ?? undefined}>{formatDateTime(v)}</time>
      ),
    },
    {
      title: '状态',
      key: 'status',
      render: (_: unknown, s: Schedule) => <ScheduleStatusTag schedule={s} />,
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (_: unknown, s: Schedule) => (
        <Switch
          checked={s.enabled}
          loading={isMutating(s.id)}
          onChange={(checked) => void toggleEnabled(s.id, checked)}
          aria-label={`${s.enabled ? '停用' : '启用'} ${s.name}`}
        />
      ),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, s: Schedule) => (
        <Space size="small">
          {/* 详情/编辑是路由 PUSH（Architecture 2.0 §57–§63）。 */}
          <Button size="small" type="link" onClick={() => navigate(`/schedules/${s.id}`)}>详情</Button>
          <Button
            size="small"
            type="link"
            loading={isMutating(s.id)}
            onClick={() => void runNow(s.id)}
          >
            立即运行
          </Button>
          <Button size="small" type="link" onClick={() => navigate(`/schedules/${s.id}/edit`)}>编辑</Button>
          <Popconfirm
            title="删除自动化？"
            description="历史执行记录会保留。"
            okText="删除"
            cancelText="取消"
            onConfirm={async () => {
              await remove(s.id);
            }}
          >
            <Button size="small" type="link" danger loading={isMutating(s.id)}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <PageSurface width="max" className="schedules-page">
      <PageHeader title="自动化" description="让智能体按计划自动执行重复工作，完成后可发送飞书消息" />

      <div className="schedules-toolbar">
        <div className="schedules-toolbar-left">
          <Segmented
            value={status}
            onChange={(v) => setStatus(v as ScheduleStatusFilter)}
            options={[
              { value: 'all', label: '全部' },
              { value: 'running', label: '运行中' },
              { value: 'paused', label: '已暂停' },
              { value: 'failed', label: '失败任务' },
            ]}
          />
          <Input.Search
            placeholder="搜索任务名称"
            allowClear
            style={{ maxWidth: 240 }}
            onSearch={setSearch}
            onChange={(e) => {
              if (!e.target.value) setSearch('');
            }}
          />
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => void reload()} aria-label="刷新列表" />
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/schedules/new')}>
            新建自动化
          </Button>
        </Space>
      </div>

      {fatalError && (
        <Alert
          type="error"
          showIcon
          message="加载自动化失败"
          description={error}
          action={<Button size="small" onClick={() => void reload()}>重试</Button>}
          style={{ marginBottom: 16 }}
        />
      )}

      {!fatalError && (
        <>
          {partialError && (
            <Alert
              type="warning"
              showIcon
              message="刷新失败，当前显示的是上次已加载数据"
              description={error}
              action={<Button size="small" onClick={() => void reload()}>重试</Button>}
              style={{ marginBottom: 16 }}
            />
          )}
          {loading && data.length === 0 ? (
            <LoadingState label="正在加载自动化…" rows={5} />
          ) : data.length === 0 ? (
            <EmptyState
              title={status === 'all' && !search ? '还没有自动化' : '没有匹配当前条件的任务'}
              description={status === 'all' && !search ? '新建一个自动化，让智能体按固定时间自动工作。' : '尝试调整搜索或筛选条件。'}
              action={status === 'all' && !search ? <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/schedules/new')}>新建自动化</Button> : undefined}
            />
          ) : (
            <div className="schedules-desktop-table">
              <Table
                rowKey="id"
                columns={columns}
                dataSource={data}
                loading={loading}
                pagination={false}
                scroll={{ x: 860 }}
              />
              {errorPhase === 'loadMore' ? (
                <div style={{ textAlign: 'center', marginTop: 16 }}>
                  <Button danger loading={loadingMore} onClick={() => void loadMore()}>
                    加载失败，点击重试
                  </Button>
                </div>
              ) : hasMore && (
                <div style={{ textAlign: 'center', marginTop: 16 }}>
                  <Button loading={loadingMore} onClick={() => void loadMore()}>
                    加载更多
                  </Button>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </PageSurface>
  );
}
