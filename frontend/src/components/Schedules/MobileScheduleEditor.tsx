import React, { useEffect, useRef, useState } from 'react';
import { MobileFullScreenDrawer } from '@/components/MobileConsole';
import type { Schedule } from '@/types/schedule';
import { useScheduleEditor } from './useScheduleEditor';
import { ScheduleEditorFields, type ScheduleEditorView } from './ScheduleEditorFields';

export interface MobileScheduleEditorProps {
  open: boolean;
  editing: Schedule | null;
  presetApplicationId?: number;
  onClose: () => void;
  onSaved: () => void;
}

const triggerFields = ['schedule_type', 'trigger', 'run_at_local', 'timezone', 'start_mode', 'end_mode', 'starts_at_local', 'ends_at_local', 'conversation_policy', 'overlap_policy', 'misfire_policy', 'deadline_policy', 'execution_window_seconds'];
function fieldView(name: string): ScheduleEditorView {
  if (name === 'application_id') return 'agent';
  if (name === 'deliveries') return 'delivery';
  if (triggerFields.includes(name)) return 'trigger';
  return 'compose';
}

export function MobileScheduleEditor({ open, editing, presetApplicationId, onClose, onSaved }: MobileScheduleEditorProps) {
  const state = useScheduleEditor({ open, editing, presetApplicationId, onSaved, onClose });
  const [view, setView] = useState<ScheduleEditorView>('compose');
  // Update identity during render, not in an effect: even a promise settling before
  // passive effects must be unable to act on a different editor (including ABA).
  const identity = `${open}:${editing?.id ?? 'new'}:${presetApplicationId ?? ''}`;
  const session = useRef({ identity, generation: 0 });
  if (session.current.identity !== identity) {
    session.current = { identity, generation: session.current.generation + 1 };
  }
  const renderGeneration = session.current.generation;
  const mounted = useRef(true);
  const actionSequence = useRef(0);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => { setView('compose'); }, [open, editing?.id, presetApplicationId]);

  function navigate(next: ScheduleEditorView) {
    actionSequence.current += 1;
    setView(next);
  }

  function revealErrors(error?: unknown) {
    const fields = (error as { errorFields?: { name: (string | number)[] }[] } | undefined)?.errorFields;
    const invalid = fields?.[0] ?? state.form.getFieldsError().find((field) => field.errors.length > 0);
    if (invalid) setView(fieldView(String(invalid.name[0])));
  }

  async function action(kind: 'save' | 'preview') {
    const isCurrentSession = () => open && mounted.current
      && session.current.generation === renderGeneration;
    if (!isCurrentSession()) return;
    const sequence = ++actionSequence.current;
    const isCurrent = () => isCurrentSession() && actionSequence.current === sequence;
    try {
      if (kind === 'save' && view !== 'compose') {
        // All controls remain mounted. Only validate the page being completed.
        const names = state.form.getFieldsError().map(({ name }) => name).filter((name) => fieldView(String(name[0])) === view);
        await state.form.validateFields(names);
        if (!isCurrent()) return;
        setView('compose');
        return;
      }
      await state.form.validateFields();
      if (!isCurrent()) return;
      if (kind === 'preview') await state.refreshPreview();
      else await state.handleOk();
      if (!isCurrent()) return;
      // Shared hook mapping validation sets errors rather than throwing.
      revealErrors();
    } catch (error) {
      if (!isCurrent()) return;
      revealErrors(error);
    }
  }

  return (
    <MobileFullScreenDrawer
      open={open}
      title={view === 'compose' ? (editing ? '编辑自动化' : '新建自动化') : { agent: '选择智能体', trigger: '触发配置', delivery: '推送配置' }[view]}
      actionText={view === 'compose' ? (editing ? '保存' : '创建') : '完成'}
      actionLoading={state.saving}
      onAction={() => void action('save')}
      onClose={() => {
        actionSequence.current += 1;
        if (view === 'compose') onClose();
        else setView('compose');
      }}
    >
      <ScheduleEditorFields state={state} mobile view={view} onNavigate={navigate} onPreview={() => action('preview')} key={`${open}:${editing?.id ?? 'new'}:${presetApplicationId ?? ''}`} />
    </MobileFullScreenDrawer>
  );
}
