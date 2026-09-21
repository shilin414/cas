/** Effective date controls. Sheet drafts never mutate the shared form until Done. */
import React, { useState } from 'react';
import { Button, Drawer, Form, Input, Radio, type FormInstance } from 'antd';
import { RightOutlined } from '@ant-design/icons';
import type { ScheduleFormValues } from '@/lib/scheduleFormat';

interface Props {
  form: FormInstance<ScheduleFormValues>;
  mobile?: boolean;
  active: boolean;
}

function BoundaryField({ form, mobile, active, kind }: Props & { kind: 'start' | 'end' }) {
  const isStart = kind === 'start';
  const modeName = isStart ? 'start_mode' : 'end_mode';
  const dateName = isStart ? 'starts_at_local' : 'ends_at_local';
  const label = isStart ? '开始时间' : '结束时间';
  const defaultMode = isStart ? 'immediate' : 'never';
  const defaultLabel = isStart ? '立即生效' : '永不结束';
  const mode = Form.useWatch(modeName, form) ?? defaultMode;
  const date = Form.useWatch(dateName, form) as string | undefined;
  const [open, setOpen] = useState(false);
  const [draftMode, setDraftMode] = useState(defaultMode);
  const [draftDate, setDraftDate] = useState('');
  const [error, setError] = useState('');
  const options = [{ value: defaultMode, label: defaultLabel }, { value: 'specified', label: '指定时间' }];
  const dateRules = [
    { required: active && mode === 'specified', message: `请选择${label}` },
    { validator: async (_: unknown, value?: string) => {
      if (!active || mode !== 'specified' || !value) return;
      if (!Number.isFinite(new Date(value).getTime())) throw new Error(`请选择有效的${label}`);
      const start = form.getFieldValue('starts_at_local') as string | undefined;
      if (!isStart && form.getFieldValue('start_mode') === 'specified' && start && value < start) {
        throw new Error('结束时间不得早于开始时间');
      }
    } },
  ];

  function openSheet() {
    setDraftMode(mode); setDraftDate(date ?? ''); setError(''); setOpen(true);
  }
  function done() {
    if (draftMode === 'specified' && (!draftDate || !Number.isFinite(new Date(draftDate).getTime()))) {
      setError(`请选择${label}`); return;
    }
    const start = form.getFieldValue('starts_at_local') as string | undefined;
    if (!isStart && draftMode === 'specified' && form.getFieldValue('start_mode') === 'specified' && start && draftDate < start) {
      setError('结束时间不得早于开始时间'); return;
    }
    form.setFieldValue(modeName, draftMode);
    form.setFieldValue(dateName, draftDate || undefined);
    setOpen(false);
  }

  return (
    <div className="automation-editor__boundary">
      <Form.Item name={modeName} label={mobile ? undefined : label} hidden={mobile}>
        {mobile ? <Input /> : <Radio.Group options={options} />}
      </Form.Item>
      <Form.Item
        name={dateName}
        label={!mobile && mode === 'specified' ? `${isStart ? '开始' : '结束'}日期和时间` : undefined}
        hidden={mobile || mode !== 'specified'}
        dependencies={['start_mode', 'starts_at_local', modeName]}
        rules={dateRules}
      >
        <Input type="datetime-local" step={60} aria-label={mobile ? `${label}已保存值` : `${isStart ? '开始' : '结束'}日期和时间`} />
      </Form.Item>
      {mobile && (
        <>
          <button type="button" className="automation-editor__row" aria-label={`设置${label}`} onClick={openSheet}>
            <span>{label}</span>
            <span className="automation-editor__row-value">{mode === 'specified' ? date?.replace('T', ' ') || '请选择' : defaultLabel}<RightOutlined /></span>
          </button>
          <Form.Item noStyle shouldUpdate>
            {() => {
              const errors = form.getFieldError(dateName);
              return errors.length > 0 ? <p role="alert" className="automation-editor__error">{errors[0]}</p> : null;
            }}
          </Form.Item>
          <Drawer
            open={open}
            onClose={() => setOpen(false)}
            placement="bottom"
            height="auto"
            title={label}
            rootClassName="automation-boundary-sheet"
            styles={{ body: { overflowY: 'auto' } }}
            footer={<div className="automation-boundary-sheet__footer"><Button onClick={() => setOpen(false)}>取消</Button><Button type="primary" data-action="done" onClick={done}>完成</Button></div>}
          >
            <Radio.Group aria-label={label} value={draftMode} onChange={(e) => { setDraftMode(e.target.value); setError(''); }} options={options} />
            {draftMode === 'specified' && <div className="automation-boundary-sheet__date">
              <label htmlFor={`automation-${kind}-draft`}>{isStart ? '开始' : '结束'}日期和时间</label>
              <Input id={`automation-${kind}-draft`} type="datetime-local" step={60} value={draftDate} aria-label={`${isStart ? '开始' : '结束'}日期和时间`} aria-invalid={!!error} aria-describedby={error ? `automation-${kind}-error` : undefined} onChange={(e) => { setDraftDate(e.target.value); setError(''); }} />
              <p className="automation-editor__hint">日期和时间使用设备本地时区；重复执行时刻使用自动化的时区。</p>
            </div>}
            {error && <p id={`automation-${kind}-error`} role="alert" className="automation-editor__error">{error}</p>}
          </Drawer>
        </>
      )}
    </div>
  );
}

export function ScheduleBoundaryFields(props: Props) {
  return <div hidden={!props.active} className="automation-editor__boundaries"><BoundaryField {...props} kind="start" /><BoundaryField {...props} kind="end" /></div>;
}
