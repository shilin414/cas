import React from 'react';
import { Modal } from 'antd';
import type { Schedule } from '@/types/schedule';
import { useScheduleEditor } from './useScheduleEditor';
import { ScheduleEditorFields } from './ScheduleEditorFields';

export interface ScheduleEditorModalProps {
  open: boolean;
  editing: Schedule | null;
  presetApplicationId?: number;
  onClose: () => void;
  onSaved?: (saved: Schedule) => void;
}

export function ScheduleEditorModal({ open, editing, presetApplicationId, onClose, onSaved }: ScheduleEditorModalProps) {
  const state = useScheduleEditor({ open, editing, presetApplicationId, onSaved: onSaved ?? (() => {}), onClose });
  return (
    <Modal
      title={editing ? '编辑自动化' : '新建自动化'}
      open={open}
      onCancel={onClose}
      onOk={() => void state.handleOk()}
      confirmLoading={state.saving}
      okText={editing ? '保存' : '创建'}
      cancelText="取消"
      width={1160}
      className="automation-editor-modal"
      destroyOnHidden
    >
      <ScheduleEditorFields state={state} key={`${editing?.id ?? 'new'}:${presetApplicationId ?? ''}`} />
    </Modal>
  );
}
