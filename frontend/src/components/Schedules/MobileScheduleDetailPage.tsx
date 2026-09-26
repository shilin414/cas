/**
 * MobileScheduleDetailPage — /schedules/:id 移动详情页（Architecture 2.0 §55）。
 *
 * 从 MobileScheduleDetail（open/onClose drawer 版）重构为路由页面：
 * open 恒 true、返回交给 Shell Header（navigation.back）。内容分节、
 * 双失败域、分页语义与原组件完全一致。
 */
import React from 'react';
import { Alert, Button, Skeleton, Tag } from 'antd';
import { MobileSection } from '@/components/MobileConsole';
import { OccurrenceStatusTag, ScheduleStatusTag } from '@/components/Schedules/ScheduleStatusTag';
import { describeSchedulePlan, describeDeliveryCondition, formatLocalDateTime } from '@/lib/scheduleFormat';
import { useScheduleDetail } from '@/components/Schedules/useScheduleDetail';
import './MobileScheduleDetail.css';

export function MobileScheduleDetailPage({ scheduleId }: { scheduleId: number }) {
  const {
    schedule, loading, error,
    occurrences, occurrencesLoading, occurrenceError,
    hasMoreOccurrences, loadingMoreOccurrences, loadMoreOccurrences,
    retryOccurrences, retrySchedule,
  } = useScheduleDetail(scheduleId);

  return (
    <div className="mobile-schedule-detail mobile-page">
      {loading && <Skeleton active />}
      {error && (
        <Alert
          type="error"
          showIcon
          message="加载自动化详情失败"
          description={error}
          action={<Button size="small" onClick={() => void retrySchedule()}>重试</Button>}
        />
      )}
      {!loading && !error && schedule && (
        <>
          <div className="mobile-schedule-detail__status">
            <ScheduleStatusTag schedule={schedule} />
          </div>

          <MobileSection title="执行计划">
            <div className="mobile-schedule-detail__kv">
              <span>{describeSchedulePlan(schedule)}</span>
              <span>
                生效开始：
                <time dateTime={schedule.trigger?.starts_at}>
                  {schedule.trigger?.starts_at ? formatLocalDateTime(schedule.trigger.starts_at) : '立即生效'}
                </time>
              </span>
              <span>
                生效结束：
                <time dateTime={schedule.trigger?.ends_at}>
                  {schedule.trigger?.ends_at ? formatLocalDateTime(schedule.trigger.ends_at) : '永不结束'}
                </time>
              </span>
              <span>
                下次执行：
                <time dateTime={schedule.next_run_at ?? undefined}>
                  {formatLocalDateTime(schedule.next_run_at)}
                </time>
              </span>
            </div>
          </MobileSection>

          <MobileSection title="任务内容">
            <p className="mobile-schedule-detail__prompt">{schedule.prompt}</p>
          </MobileSection>

          {schedule.deliveries && schedule.deliveries.length > 0 && (
            <MobileSection title="通知">
              <div className="mobile-schedule-detail__kv">
                {schedule.deliveries.map((d) => (
                  <div key={d.id} data-delivery-id={d.id}>
                    <Tag>{d.target_name || d.target_id}</Tag>
                    <p className="mobile-schedule-detail__condition">{describeDeliveryCondition(d.condition)}</p>
                  </div>
                ))}
              </div>
            </MobileSection>
          )}

          {/* 执行记录：独立失败域（§37）——历史接口挂了不影响任务配置。 */}
          <MobileSection title="执行记录" flush>
            {occurrenceError && occurrences.length === 0 ? (
              <div className="mobile-console-empty">
                <strong>加载执行记录失败</strong>
                <Button onClick={() => void retryOccurrences()}>重试</Button>
              </div>
            ) : occurrencesLoading && occurrences.length === 0 ? (
              <Skeleton active title={false} paragraph={{ rows: 3 }} />
            ) : occurrences.length === 0 ? (
              <div className="mobile-console-empty">还没有执行记录</div>
            ) : (
              <>
                {occurrences.map((occ) => (
                  <div key={occ.id} className="mobile-schedule-detail__occ">
                    <div className="mobile-schedule-detail__occ-head">
                      <OccurrenceStatusTag status={occ.status} />
                      <time dateTime={occ.scheduled_at}>{formatLocalDateTime(occ.scheduled_at)}</time>
                    </div>
                    {(occ.enqueued_at || occ.finished_at) && (
                      <div className="mobile-schedule-detail__occ-meta">
                        {occ.enqueued_at && <span>入队 {formatLocalDateTime(occ.enqueued_at)}</span>}
                        {occ.finished_at && <span>完成 {formatLocalDateTime(occ.finished_at)}</span>}
                      </div>
                    )}
                  </div>
                ))}
                {occurrenceError ? (
                  <button
                    type="button"
                    className="mobile-console-more"
                    onClick={() => void loadMoreOccurrences()}
                  >
                    {loadingMoreOccurrences ? '加载中…' : '加载失败，点击重试'}
                  </button>
                ) : hasMoreOccurrences && (
                  <button
                    type="button"
                    className="mobile-console-more"
                    onClick={() => void loadMoreOccurrences()}
                  >
                    {loadingMoreOccurrences ? '加载中…' : '加载更多执行记录'}
                  </button>
                )}
              </>
            )}
          </MobileSection>
        </>
      )}
    </div>
  );
}
