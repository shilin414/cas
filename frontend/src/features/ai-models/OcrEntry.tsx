import React, { useState } from 'react';
import { Alert, Button, Empty } from 'antd';
import { ScanOutlined } from '@ant-design/icons';
import type { AIModel } from '@/services/aiModels';
import { errorText } from './modelLogic';

// OCR is temporarily disabled for deployment (2026-09): the panel never loads and
// the build needs no OCR runtime/model assets (fully offline, smaller image).
// To re-enable:
//   1. npm run ocr:enable   (restores the old predev/prebuild hook behavior)
//   2. restore the dynamic import below:
//        export const loadOcrPanel = () => import('./ocr/OcrTestPanel');
type OcrPanelModule = typeof import('./ocr/OcrTestPanel'); // type-only: erased at build
export const loadOcrPanel = (): Promise<OcrPanelModule> =>
  Promise.reject(new Error('本地 OCR 组件已在当前版本临时下线'));
class OcrBoundary extends React.Component<{ children: React.ReactNode }, { error: string }> {
  state = { error: '' };
  static getDerivedStateFromError(error: unknown) { return { error: errorText(error) }; }
  render() { return this.state.error ? <Alert type="error" showIcon message="本地识别组件运行失败" description={this.state.error} /> : this.props.children; }
}
export default function OcrEntry({ model, allowed, loader = loadOcrPanel }: { model: AIModel; allowed: boolean; loader?: typeof loadOcrPanel }) {
  const [Panel, setPanel] = useState<React.ComponentType<{ model: AIModel }> | null>(null);
  const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  if (Panel && allowed && model.enabled) return <OcrBoundary><Panel model={model} /></OcrBoundary>;
  return <div className="aim-local-entry">
    <div className="aim-local-icon"><ScanOutlined /></div><h3>在设备上识别，不上传原图</h3>
    <p>本地 OCR 尚未加载。开始后才会加载识别组件及其所需资源，实际下载量由资源清单决定。</p>
    <div className="aim-inline-meta"><span>可信适配器 · paddleocr_tiny</span><span>资源版本 · {model.browser_manifest?.version || '未配置'}</span></div>
    {!allowed && <Alert type="warning" message="需要 ai.model.test 权限" />}
    {!model.enabled && <Alert type="warning" message="模型已停用" />}
    {!model.browser_manifest && <Empty description="尚未配置 OCR 资源清单，请先编辑模型" />}
    {error && <Alert type="error" message="识别组件加载失败，可再次尝试" description={error} />}
    <Button type="primary" icon={<ScanOutlined />} disabled={!allowed || !model.enabled || !model.browser_manifest} loading={busy} onClick={async () => { if (busy || !allowed || !model.enabled) return; setBusy(true); setError(''); try { const module = await loader(); setPanel(() => module.default); } catch (e) { setError(errorText(e)); } finally { setBusy(false); } }}>开始本地识别</Button>
  </div>;
}
