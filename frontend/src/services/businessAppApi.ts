import axiosInstance from './axios';
import type { FeishuForwardResult } from './shareApi';
import type { BusinessInput } from '@/components/BusinessApps/model';
export interface BusinessStatus { configured: boolean; account: string; can_manage_others: boolean; can_submit: boolean; reason?: string }
export interface QuerySnapshotInfo { token: string; query_label: string; query_value: string; queried_at: string; expires_at: string; can_forward: boolean }
export interface BusinessResult { data: unknown; message: string; snapshot?: QuerySnapshotInfo; forward_unavailable?: string }
export const businessAppApi = {
 snapshot: (id: number, token: string, signal: AbortSignal): Promise<BusinessResult> => axiosInstance.get(`/v2/applications/${id}/business/snapshots/${encodeURIComponent(token)}`, { signal, silentError: true }) as Promise<BusinessResult>,
 forward: (id: number, token: string, targets: { target_type: 'user' | 'chat'; id: string }[]): Promise<FeishuForwardResult> => axiosInstance.post(`/v2/applications/${id}/business/forward`, { snapshot_token: token, targets }, { timeout: 55000, silentError: true }) as Promise<FeishuForwardResult>,
 status: (id: number, signal: AbortSignal): Promise<BusinessStatus> => axiosInstance.get(`/v2/applications/${id}/business/status`, { signal, silentError: true }) as Promise<BusinessStatus>,
 execute: (id: number, input: BusinessInput, signal: AbortSignal): Promise<BusinessResult> => axiosInstance.post(`/v2/applications/${id}/business/execute`, input, { signal, timeout: 25000, silentError: true }) as Promise<BusinessResult>,
};
