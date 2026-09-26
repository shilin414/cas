/**
 * MobileScheduleCenter — 自动化移动端（Architecture 2.0 §54–§55）。
 *
 * 与桌面共用 useSchedules（业务层绝不复制）。详情/编辑不再是本地 state：
 *   · 点击卡片 → pushPage('/schedules/:id')（真实 History，侧滑返回）；
 *   · 编辑 → pushPage('/schedules/:id/edit')；
 *   ••• ActionSheet 保留 立即运行/启停/删除（真正的临时 Overlay）。
 */
import React, { useCallback, useState } from 'react';
import { Alert, Button, Modal, Segmented, Skeleton } from 'antd';
import {
  CaretRightOutlined,
  DeleteOutlined,
  EditOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
} from '@ant-design/icons';
import { useSchedules } from '@/hooks/useSchedules';
import { useMobileHeaderAction } from '@/shell/mobileHeader';
import { useAppNavigation } from '@/router/useAppNavigation';
import type { Schedule, ScheduleStatusFilter } from '@/types/schedule';
import { ScheduleStatusTag } from '@/components/Schedules/ScheduleStatusTag';
import {
  MobileActionSheet,
  MobileEmptyState,
  MobilePage,
  MobileSearchBar,
  type MobileAction,
} from '@/components/MobileConsole';
import { describeSchedulePlan, formatDateTime } from '@/lib/scheduleFormat';
import './SchedulesPage.css';

function ScheduleCardSkeleton() {
  return (
    <div className="mobile-schedule-card">
      <Skeleton active title paragraph={{ rows: 2 }} />
    </div>
  );
}

export function MobileScheduleCenter() {
  const {
    data, loading, loadingMore, error, errorPhase, hasMore, loadMore,
    status, search, isMutating,
    setStatus, setSearch, reload, toggleEnabled, runNow, remove,
  } = useSchedules();
  const navigation = useAppNavigation();
  const [sheetFor, setSheetFor] = useState<Schedule | null>(null);

  // 错误分类（三次复审 §30–§31）：fatal = 第一页失败且无数据；
  // partial = 已有数据时刷新失败 → 警告 + 旧数据继续展示。
  const fatalError = Boolean(error) && data.length === 0;
  const partialError = Boolean(error) && data.length > 0
    && errorPhase !== 'loadMore';

  // 顶栏 ＋：路由进入新建页（真实 URL，可返回/刷新）。
  const openNew = useCallback(() => {
    navigation.pushPage('/schedules/new');
  }, [navigation]);
  useMobileHeaderAction({ onAction: openNew });

  const confirmRemove = (s: Schedule) => {
    Modal.confirm({
      title: '删除自动化？',
      content: '历史执行记录会保留。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => remove(s.id),
    });
  };

  const actions: MobileAction[] = sheetFor ? [
    {
      key: 'run', label: '立即运行', icon: <CaretRightOutlined />,
      disabled: isMutating(sheetFor.id),
      onClick: () => void runNow(sheetFor.id),
    },
    {
      key: 'edit', label: '编辑', icon: <EditOutlined />,
      onClick: () => navigation.pushPage(`/schedules/${sheetFor.id}/edit`),
    },
    {
      key: 'toggle',
      label: sheetFor.enabled ? '暂停' : '启用',
      icon: sheetFor.enabled ? <PauseCircleOutlined /> : <PlayCircleOutlined />,
      disabled: isMutating(sheetFor.id),
      onClick: () => void toggleEnabled(sheetFor.id, !sheetFor.enabled),
    },
    {
      key: 'remove', label: '删除', icon: <DeleteOutlined />, danger: true,
      disabled: isMutating(sheetFor.id),
      onClick: () => confirmRemove(sheetFor),
    },
  ] : [];

  return (
    <MobilePage>
      <div className="mobile-console-page__sticky">
        <Segmented
          block
          value={status}
          onChange={(v) => setStatus(v as ScheduleStatusFilter)}
          options={[
            { value: 'all', label: '全部' },
            { value: 'running', label: '运行中' },
            { value: 'paused', label: '已暂停' },
            { value: 'failed', label: '失败' },
          ]}
        />
        <MobileSearchBar
          placeholder="搜索任务名称"
          value={search}
          onChange={setSearch}
        />
      </div>

      {fatalError && (
        <Alert
          type="error"
          showIcon
          message="加载自动化失败"
          description={error}
          action={<Button size="small" onClick={() => void reload()}>重试</Button>}
          style={{ marginTop: 12 }}
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
              style={{ marginTop: 12 }}
            />
          )}
          {loading && data.length === 0 ? (
            <div className="mobile-schedule-list">
              <ScheduleCardSkeleton />
              <ScheduleCardSkeleton />
              <ScheduleCardSkeleton />
            </div>
          ) : data.length === 0 ? (
            status === 'all' && !search ? (
              <MobileEmptyState
                title="还没有自动化"
                hint="创建一个任务，让智能体自动完成重复工作"
                action={(
                  <Button type="primary" onClick={openNew}>
                    新建自动化
                  </Button>
                )}
              />
            ) : (
              <MobileEmptyState title="没有匹配当前条件的任务" />
            )
          ) : (
            <div className="mobile-schedule-list">
              {data.map((s) => (
                <div key={s.id} className="mobile-schedule-card">
                  {/* 点击卡片 → 详情路由 PUSH（真实 History）。 */}
                  <button
                    type="button"
                    className="mobile-schedule-card__main"
                    aria-label={`查看任务详情：${s.name}`}
                    onClick={() => navigation.pushPage(`/schedules/${s.id}`)}
                  >
                    <div className="mobile-schedule-card__row">
                      <span className="mobile-schedule-card__name">{s.name}</span>
                      <ScheduleStatusTag schedule={s} compact />
                    </div>
                    <div className="mobile-schedule-card__meta">
                      <span>{describeSchedulePlan(s)}</span>
                      <span>
                        下次执行：
                        <time dateTime={s.next_run_at ?? undefined}>
                          {formatDateTime(s.next_run_at)}
                        </time>
                      </span>
                    </div>
                  </button>
                  <button
                    type="button"
                    className="mobile-schedule-card__more"
                    aria-label={`更多操作：${s.name}`}
                    onClick={() => setSheetFor(s)}
                  >
                    •••
                  </button>
                </div>
              ))}
              {errorPhase === 'loadMore' ? (
                <button
                  type="button"
                  className="mobile-console-more"
                  onClick={() => void loadMore()}
                >
                  {loadingMore ? '加载中…' : '加载失败，点击重试'}
                </button>
              ) : hasMore && (
                <button
                  type="button"
                  className="mobile-console-more"
                  onClick={() => void loadMore()}
                >
                  {loadingMore ? '加载中…' : '加载更多'}
                </button>
              )}
            </div>
          )}
        </>
      )}

      <MobileActionSheet
        open={sheetFor !== null}
        title={sheetFor?.name}
        actions={actions}
        onClose={() => setSheetFor(null)}
      />
    </MobilePage>
  );
}
