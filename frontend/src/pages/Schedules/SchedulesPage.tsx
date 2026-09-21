/**
 * SchedulesPage — 自动化中心（Console 页面）。
 * Desktop 表格 / Mobile 全新移动中心按 React 层切换（开发执行报告 §11/§23），
 * 共用 useSchedules 与编辑器状态机，仅信息架构不同。
 */
import React, { useState } from 'react';
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
import { useSchedules } from '@/hooks/useSchedules';
import { useIsMobile } from '@/shell/useIsMobile';
import { ScheduleEditorModal } from '@/components/Schedules/ScheduleEditorModal';
import { ScheduleDetailDrawer } from '@/components/Schedules/ScheduleDetailDrawer';
import { ScheduleStatusTag } from '@/components/Schedules/ScheduleStatusTag';
import { EmptyState, LoadingState, PageHeader, PageSurface } from '@/components/ProductUI';
import { describeSchedulePlan, formatDateTime } from '@/lib/scheduleFormat';
import type { Schedule, ScheduleStatusFilter } from '@/types/schedule';
import { MobileScheduleCenter } from './MobileScheduleCenter';
import './SchedulesPage.css';

/** Desktop 表格 — 保留原样（开发执行报告 §54）。 */
function DesktopScheduleCenter() {
  const {
    data, loading, loadingMore, error, errorPhase, hasMore, loadMore,
    status, search, isMutating,
    setStatus, setSearch, reload, toggleEnabled, runNow, remove,
  } = useSchedules();
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Schedule | null>(null);
  const [detailId, setDetailId] = useState<number | null>(null);

  // 错误分类（三次复审 §30–§31）：fatal = 第一页就失败且没有任何数据；
  // partial = 已有数据时刷新失败 → 警告 + 旧数据继续展示；loadMore 失败
  // → 底部唯一 CTA 变重试。已有任务绝不能因为一次刷新失败从 UI 消失。
  const fatalError = Boolean(error) && data.length === 0;
  const partialError = Boolean(error) && data.length > 0
    && errorPhase !== 'loadMore';

  const openNew = () => {
    setEditing(null);
    setEditorOpen(true);
  };
  const openEdit = (s: Schedule) => {
    setEditing(s);
    setEditorOpen(true);
  };

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
          <Button size="small" type="link" onClick={() => setDetailId(s.id)}>详情</Button>
          <Button
            size="small"
            type="link"
            loading={isMutating(s.id)}
            onClick={() => void runNow(s.id)}
          >
            立即运行
          </Button>
          <Button size="small" type="link" onClick={() => openEdit(s)}>编辑</Button>
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
          <Button type="primary" icon={<PlusOutlined />} onClick={openNew}>
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
          {/* partial：刷新失败，旧数据保留 + 警告（三次复审 §31）。 */}
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
              action={status === 'all' && !search ? <Button type="primary" icon={<PlusOutlined />} onClick={openNew}>新建自动化</Button> : undefined}
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
              {/* 唯一 CTA（三次复审 §32）：翻页失败 → 重试；否则加载更多。 */}
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

      <ScheduleEditorModal
        open={editorOpen}
        editing={editing}
        onClose={() => setEditorOpen(false)}
        onSaved={() => void reload()}
      />
      <ScheduleDetailDrawer
        open={detailId !== null}
        scheduleId={detailId}
        onClose={() => setDetailId(null)}
      />
    </PageSurface>
  );
}

export function SchedulesPage() {
  const isMobile = useIsMobile();
  return isMobile ? <MobileScheduleCenter /> : <DesktopScheduleCenter />;
}
