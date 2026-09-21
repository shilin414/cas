import { api } from "@/services/api";

export type AccessMode = "all" | "assigned" | "admin_only";
export interface AdminPermission { id: number; code: string; category: string; name: string; description: string; created_at: string; }
export interface AdminRole { id: number; code: string; name: string; description: string; is_system: boolean; enabled: boolean; permissions: AdminPermission[]; created_at: string; updated_at: string; }
export interface AdminMe { can_access_console: boolean; is_super_admin: boolean; roles: AdminRole[]; permissions: AdminPermission[]; }
export interface Administrator { user_id: number; username: string; display_name: string; email: string; auth_source: string; is_staff: boolean; is_active: boolean; last_login_at?: string | null; assignments: Array<{ id: number; role_id: number; role_code: string; role_name: string; enabled: boolean; expires_at?: string | null }>; }
export interface AccessGroup { id: number; code: string; name: string; description: string; source_type: "local" | "feishu"; external_group_id: string; external_group_type: "" | "normal" | "dynamic" | "unknown"; enabled: boolean; sync_status: string; last_synced_at?: string | null; member_count: number; covered_users: number; application_count: number; departments?: Array<{ department_id: number; name: string; include_children: boolean; covered_users: number }>; users?: Array<{ directory_user_id: number; name: string; avatar_url: string }>; created_at: string; updated_at: string; }
export interface AccessDecision { allowed: boolean; reason_code: string; application?: { id: number; name: string; enabled: boolean; access_mode: AccessMode }; user?: { id: number; name: string; active: boolean; resigned: boolean; linked: boolean }; matched_grants: Array<{ type: string; id: number; name: string; source_type?: string; external_group_type?: string; include_children?: boolean }>; access_path: string[]; }
export interface DirectoryDepartment {
  id: number;
  open_department_id: string;
  name: string;
  parent_id?: number | null;
  parent_open_department_id: string;
  order_weight: string;
  is_active: boolean;
  last_synced_at?: string | null;
}
export interface DirectoryUser {
  id: number;
  open_id: string;
  name: string;
  avatar_url: string;
  active_status: number;
  is_resigned: boolean;
  local_user_id?: number | null;
  is_active: boolean;
  departments: Array<{ id: number; name: string; is_primary: boolean }>;
}
export interface DirectoryDepartmentPage {
  results: DirectoryDepartment[];
  next_cursor?: string | null;
}
export interface DirectoryUserPage {
  results: DirectoryUser[];
  next_cursor?: string | null;
}
export type SyncScheduleType = "interval" | "daily";
export type SyncJobStatus = "pending" | "blocked" | "running" | "success" | "failed";
export interface SyncScheduleConfig {
  enabled: boolean;
  schedule_type: SyncScheduleType;
  interval_minutes: number;
  daily_time: string;
  timezone: string;
  next_run_at?: string | null;
  last_run_at?: string | null;
  last_success_at?: string | null;
  updated_at?: string;
}
export interface SyncTargetConfig extends SyncScheduleConfig {
  target_code: string;
  target_version: number;
  last_error_code: string;
  last_error_message: string;
  updated_at: string;
}
export interface SyncTarget {
  code: string;
  display_name: string;
  description: string;
  dependencies: string[];
  metrics_schema: string[];
  config?: SyncTargetConfig;
}
export type SyncTargetsResponse = SyncTarget[];
export interface SyncWarning {
  code: string;
  message: string;
  count?: number;
}
export interface SyncJob {
  id: number;
  target_code: string;
  batch_id?: number | null;
  trigger_type: string;
  status: SyncJobStatus;
  metrics: Record<string, number>;
  warnings: SyncWarning[];
  target_version: number;
  started_at?: string | null;
  finished_at?: string | null;
  error_code: string;
  error_message: string;
  created_by?: number | null;
  created_at: string;
}
export type SyncJobsResponse = SyncJob[];
export interface SyncBatch {
  id: number;
  trigger_type: string;
  status: "pending" | "running" | "partial_success" | "success" | "failed";
  requested_targets: string[];
  created_by?: number | null;
  created_at: string;
  finished_at?: string | null;
  jobs: SyncJob[];
}

// Legacy combined directory sync contract, retained only for explicit read-only
// capability detection while older deployments are phased out.
export interface SyncConfig extends SyncScheduleConfig {
  directory_version: number;
  updated_at: string;
}
export interface SyncRun {
  id: number;
  trigger_type: "manual" | "scheduled";
  status: "pending" | "running" | "success" | "failed";
  departments_count: number;
  users_count: number;
  active_users_count: number;
  memberships_count: number;
  active_memberships_count: number;
  groups_count: number;
  group_members_count: number;
  group_fetch_warnings: number;
  member_mapping_errors: number;
  unknown_users_count: number;
  started_at?: string | null;
  finished_at?: string | null;
  error_code: string;
  error_message: string;
  created_at: string;
}
export interface AccessPolicy {
  application_id: number;
  access_mode: AccessMode;
  departments: Array<{
    department_id: number;
    name: string;
    include_children: boolean;
    covered_users: number;
  }>;
  groups?: Array<{ group_id: number; name: string; source_type: string; external_group_type: string; member_count: number; enabled: boolean }>;
  users: Array<{
    directory_user_id: number;
    name: string;
    avatar_url: string;
    departments: string[];
  }>;
}
export interface DirectoryStats {
  departments_total: number;
  departments_active: number;
  users_total: number;
  users_active: number;
  users_resigned: number;
  oauth_users: number;
  linked_directory_users: number;
}
export interface AuditLog {
  id: number;
  user_id?: number | null;
  action: string;
  resource_type: string;
  resource_id: string;
  detail: Record<string, unknown>;
  created_at: string;
}

const inflightReads = new Map<string, Promise<unknown>>();

export interface EnterpriseReadOptions {
  fresh?: boolean;
  silentError?: boolean;
}

function readKey(
  url: string,
  params?: Record<string, unknown>,
  options?: EnterpriseReadOptions,
): string {
  return `${url}?${JSON.stringify(params ?? {})}&silent=${options?.silentError === true}`;
}

function invalidateReadPrefixes(prefixes: string[]): void {
  for (const key of inflightReads.keys()) {
    if (prefixes.some((prefix) => key.startsWith(prefix))) inflightReads.delete(key);
  }
}

function invalidateSyncReads(): void {
  invalidateReadPrefixes(["/v2/admin/sync-", "/v2/admin/directory/sync-"]);
}

function dedupedGet<T>(
  url: string,
  params?: Record<string, unknown>,
  options?: EnterpriseReadOptions,
): Promise<T> {
  const key = readKey(url, params, options);
  if (options?.fresh) inflightReads.delete(key);
  const existing = inflightReads.get(key) as Promise<T> | undefined;
  if (existing) return existing;
  const request = api.get<T>(url, params, { silentError: options?.silentError === true });
  inflightReads.set(key, request);
  const clear = () => { if (inflightReads.get(key) === request) inflightReads.delete(key); };
  void request.then(clear, clear);
  return request;
}

export const enterpriseApi = {
  adminMe: () => dedupedGet<AdminMe>("/v2/admin/me"),
  permissions: () => dedupedGet<AdminPermission[]>("/v2/admin/permissions"),
  roles: () => dedupedGet<AdminRole[]>("/v2/admin/roles"),
  createRole: (data: { code: string; name: string; description: string; enabled: boolean; permission_codes: string[] }) => api.post<AdminRole>("/v2/admin/roles", data),
  updateRole: (id: number, data: { code?: string; name: string; description: string; enabled: boolean; permission_codes: string[] }) => api.patch<AdminRole>(`/v2/admin/roles/${id}`, data),
  administrators: () => dedupedGet<Administrator[]>("/v2/admin/administrators"),
  updateAdministrator: (userId: number, roleIds: number[], expiresAt?: string | null) => api.patch<Administrator>(`/v2/admin/administrators/${userId}`, { user_id: userId, role_ids: roleIds, expires_at: expiresAt ?? null }),
  accessGroups: (sourceType?: "local" | "feishu") => dedupedGet<AccessGroup[]>("/v2/admin/access-groups", sourceType ? { source_type: sourceType } : undefined),
  accessGroup: (id: number) => dedupedGet<AccessGroup>(`/v2/admin/access-groups/${id}`),
  createAccessGroup: (data: { code: string; name: string; description: string; enabled: boolean; department_grants: Array<{ department_id: number; include_children: boolean }>; user_grants: number[] }) => api.post<AccessGroup>("/v2/admin/access-groups", data),
  updateAccessGroup: (id: number, data: { code?: string; name: string; description: string; enabled: boolean; department_grants: Array<{ department_id: number; include_children: boolean }>; user_grants: number[] }) => api.patch<AccessGroup>(`/v2/admin/access-groups/${id}`, data),
  deleteAccessGroup: (id: number) => api.delete<void>(`/v2/admin/access-groups/${id}`),
  diagnose: (directoryUserId: number, applicationId: number) => api.post<AccessDecision>("/v2/admin/access/diagnose", { directory_user_id: directoryUserId, application_id: applicationId }),
  stats: () => dedupedGet<DirectoryStats>("/v2/admin/directory/stats"),
  departments: (params?: Record<string, unknown>) =>
    dedupedGet<DirectoryDepartment[]>("/v2/admin/directory/departments", params),
  departmentPage: (params?: Record<string, unknown>) =>
    dedupedGet<DirectoryDepartmentPage>("/v2/admin/directory/departments/page", params),
  users: (params?: Record<string, unknown>) =>
    dedupedGet<DirectoryUserPage>("/v2/admin/directory/users", params),
  syncTargets: (options?: EnterpriseReadOptions) =>
    dedupedGet<SyncTargetsResponse>("/v2/admin/sync-targets", undefined, options),
  syncTargetConfig: (code: string, options?: EnterpriseReadOptions) =>
    dedupedGet<SyncTargetConfig>(`/v2/admin/sync-targets/${encodeURIComponent(code)}/config`, undefined, options),
  updateSyncTargetConfig: async (code: string, data: Partial<SyncScheduleConfig>) => {
    const result = await api.put<SyncTargetConfig>(`/v2/admin/sync-targets/${encodeURIComponent(code)}/config`, data);
    invalidateSyncReads();
    return result;
  },
  triggerSyncTarget: async (code: string) => {
    const result = await api.post<SyncJob>(`/v2/admin/sync-targets/${encodeURIComponent(code)}/jobs`);
    invalidateSyncReads();
    return result;
  },
  triggerSyncBatch: async (targets: string[]) => {
    const result = await api.post<SyncBatch>("/v2/admin/sync-batches", { targets });
    invalidateSyncReads();
    return result;
  },
  syncJobs: (limit = 50, options?: EnterpriseReadOptions) =>
    dedupedGet<SyncJobsResponse>("/v2/admin/sync-jobs", { limit }, options),
  // Legacy combined endpoints. syncManagement.ts uses them only when the
  // extensible API is unavailable during rollout.
  syncConfig: (options?: EnterpriseReadOptions) => dedupedGet<SyncConfig>("/v2/admin/directory/sync-config", undefined, options),
  updateSyncConfig: (data: Partial<SyncConfig>) =>
    api.put<SyncConfig>("/v2/admin/directory/sync-config", data),
  triggerSync: () => api.post<SyncRun>("/v2/admin/directory/sync"),
  syncRuns: (limit = 50, options?: EnterpriseReadOptions) =>
    dedupedGet<SyncRun[]>("/v2/admin/directory/sync-runs", { limit }, options),
  access: (id: number) =>
    dedupedGet<AccessPolicy>(`/v2/admin/applications/${id}/access`),
  updateAccess: (
    id: number,
    data: {
      access_mode: AccessMode;
      department_grants: Array<{
        department_id: number;
        include_children: boolean;
      }>;
      user_grants: number[];
      group_grants: number[];
    },
  ) => api.put<AccessPolicy>(`/v2/admin/applications/${id}/access`, data),
  audits: (limit = 100) =>
    dedupedGet<AuditLog[]>("/v2/admin/audit-logs", { limit }),
};
