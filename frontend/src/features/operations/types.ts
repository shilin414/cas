export const RUN_STATUSES = ['active', 'all', 'queued', 'running', 'waiting_input', 'waiting_external', 'cancelling', 'cancelled', 'succeeded', 'failed', 'interrupted'] as const;
export type RunStatusFilter = typeof RUN_STATUSES[number];
export interface RunsQuery {
  status?: RunStatusFilter;
  provider?: string;
  owner_user_id?: string;
  application_id?: string;
  limit?: number;
  cursor?: string;
}
export interface ProviderCapacity {
  key: string;
  name: string;
  status: string;
  max_inflight: number;
  effective_inflight: number;
  controlled_inflight: number;
  uncontrolled_inflight: number;
  queued: number;
  running: number;
  waiting_input: number;
  waiting_external: number;
  cancelling: number;
  oldest_queued_at: string | null;
  oldest_queued_age_seconds: number;
}
export interface OperationsOverview {
  sampled_at: string;
  providers: ProviderCapacity[];
  totals: {
    queued: number; running: number; waiting_input: number; waiting_external: number; cancelling: number;
    pending_occurrences: number; pending_deliveries: number; sending_deliveries: number; pending_outbox: number;
  };
  alerts: Array<{ code: string; severity: 'warning' | 'critical'; provider?: string; message: string }>;
  limitations: string[];
}
export interface OperationRun {
  id: string;
  provider: string;
  runtime_type: string;
  status: string;
  priority: string;
  trigger_type: string;
  owner_user_id: string | null;
  application_id: string | null;
  conversation_id: string | null;
  queued_at: string;
  available_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
  attempt: number;
  max_attempts: number;
  error_code: string;
  queue_age_seconds: number | null;
  wait_reason: string;
}
export interface RunsPage { results: OperationRun[]; next_cursor: string | null }
// expected_max_inflight may be zero when repairing an invalid existing configuration.
export interface CapacityChange { max_inflight: number; expected_max_inflight: number; reason: string }
export interface CapacityChangeResult {
  provider: string;
  previous_max_inflight: number;
  max_inflight: number;
  effective_for: 'new_admissions';
  updated_at: string;
}
