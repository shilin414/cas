/**
 * ScheduleEditorRoute — /schedules/new 与 /schedules/:id/edit
 * （Architecture 2.0 §59–§62）。
 *
 * Mobile：全屏页面（复用 ScheduleEditorFields + useScheduleEditor 状态机），
 * 内部 step（compose/agent/trigger/delivery）是 Local State，返回由
 * useMobileBackOverride 接管：非 compose → 回 compose；compose → back()。
 * Desktop：列表背景 + Editor Modal（视觉不变，开关由路由决定）。
 *
 * 保存成功语义（§62）：
 *   new   → replace('/schedules/:newId')
 *   edit  → replace('/schedules/:id')
 * 避免 Back 回到已提交的表单。
 */
import React, { useState } from 'react';
import { Result } from 'antd';
import { useParams } from 'react-router-dom';
import type { Schedule } from '@/types/schedule';
import { useIsMobile } from '@/shell/useIsMobile';
import { useAppNavigation } from '@/router/useAppNavigation';
import { useMobileBackOverride } from '@/shell/mobileHeader';
import { parsePositiveRouteId } from '@/router/routeParams';
import { useScheduleEditor } from '@/components/Schedules/useScheduleEditor';
import {
  ScheduleEditorFields,
  type ScheduleEditorView,
} from '@/components/Schedules/ScheduleEditorFields';
import { ScheduleEditorModal } from '@/components/Schedules/ScheduleEditorModal';
import { DesktopScheduleCenter } from './DesktopScheduleCenter';
import './SchedulesPage.css';

const TRIGGER_FIELDS = [
  'schedule_type', 'trigger', 'run_at_local', 'timezone', 'start_mode',
  'end_mode', 'starts_at_local', 'ends_at_local', 'conversation_policy',
  'overlap_policy', 'misfire_policy', 'deadline_policy', 'execution_window_seconds',
];

function fieldView(name: string): ScheduleEditorView {
  if (name === 'application_id') return 'agent';
  if (name === 'deliveries') return 'delivery';
  if (TRIGGER_FIELDS.includes(name)) return 'trigger';
  return 'compose';
}

/** Mobile 全屏编辑页面：内部 step 是 Local State，不是路由。 */
function MobileScheduleEditorPage({ mode, scheduleId }: {
  mode: 'create' | 'edit';
  scheduleId: number | null;
}) {
  const navigation = useAppNavigation();
  const [view, setView] = useState<ScheduleEditorView>('compose');
  // 路由只携带 id；编辑器通过 fetchSchedule hydration 拿到完整配置。
  const [editing] = useState<Schedule | null>(scheduleId ? ({ id: scheduleId } as Schedule) : null);

  // 保存成功（§62）：new → replace 新详情页；edit → replace 详情页。
  // onSaved 携带服务器返回的 Schedule（含新 id）。
  const state = useScheduleEditor({
    open: true,
    editing,
    onSaved: (saved) => {
      navigation.replacePage(`/schedules/${saved.id}`);
    },
    onClose: () => navigation.back(),
  });

  // 返回（§61）：非 compose → 回 compose；compose → back()。
  useMobileBackOverride({
    active: view !== 'compose',
    onBack: () => setView('compose'),
  });

  const revealErrors = (error?: unknown) => {
    const fields = (error as { errorFields?: { name: (string | number)[] }[] } | undefined)?.errorFields;
    const invalid = fields?.[0] ?? state.form.getFieldsError().find((field) => field.errors.length > 0);
    if (invalid) setView(fieldView(String(invalid.name[0])));
  };

  const action = async (kind: 'save' | 'preview') => {
    try {
      if (kind === 'save' && view !== 'compose') {
        // 只校验当前正在完成的分页（与原 MobileScheduleEditor 相同）。
        const names = state.form.getFieldsError().map(({ name }) => name)
          .filter((name) => fieldView(String(name[0])) === view);
        await state.form.validateFields(names);
        setView('compose');
        return;
      }
      await state.form.validateFields();
      if (kind === 'preview') await state.refreshPreview();
      else await state.handleOk();
      revealErrors();
    } catch (error) {
      revealErrors(error);
    }
  };

  return (
    <div className="mobile-page mobile-schedule-editor">
      <ScheduleEditorFields
        state={state}
        mobile
        view={view}
        onNavigate={setView}
        onPreview={() => void action('preview')}
        key={`${mode}:${scheduleId ?? 'new'}`}
      />
      <div className="mobile-schedule-editor__bar">
        <button
          type="button"
          className="mobile-schedule-editor__primary"
          disabled={state.saving}
          onClick={() => void action('save')}
        >
          {state.saving ? '保存中…' : view === 'compose' ? (mode === 'edit' ? '保存' : '创建') : '完成'}
        </button>
      </div>
    </div>
  );
}

/** Desktop：列表背景 + Editor Modal（路由驱动开关）。 */
function DesktopEditorOverlay({ mode, scheduleId }: {
  mode: 'create' | 'edit';
  scheduleId: number | null;
}) {
  const navigation = useAppNavigation();
  // 路由只携带 id；编辑器通过 fetchSchedule hydration 拿到完整配置。
  const [editing] = useState<Schedule | null>(scheduleId ? ({ id: scheduleId } as Schedule) : null);
  const [reloadKey, setReloadKey] = useState(0);

  return (
    <>
      <DesktopScheduleCenter key={reloadKey} />
      <ScheduleEditorModal
        open
        editing={editing}
        onClose={() => navigation.back()}
        onSaved={() => setReloadKey((k) => k + 1)}
      />
    </>
  );
}

export default function ScheduleEditorRoute({ mode }: { mode: 'create' | 'edit' }) {
  const { scheduleId } = useParams();
  const isMobile = useIsMobile();
  const id = mode === 'edit' ? parsePositiveRouteId(scheduleId) : null;

  if (mode === 'edit' && id === null) {
    return <Result status="404" title="任务不存在" subTitle="任务地址无效。" />;
  }

  if (isMobile) {
    return <MobileScheduleEditorPage mode={mode} scheduleId={id} />;
  }
  return <DesktopEditorOverlay mode={mode} scheduleId={id} />;
}
