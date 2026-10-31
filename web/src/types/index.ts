// ─── Organizations & Applications ───────────────────────────────────────────

export interface Organization {
  id: string;
  name: string;
  created_at: string;
}

export interface Application {
  id: string;
  organization_id: string;
  name: string;
  description?: string;
  created_at: string;
}

// ─── Tenants ─────────────────────────────────────────────────────────────────

export type TenantStatus = 'active' | 'suspended' | 'inactive';

export interface Tenant {
  id: string;
  organization_id: string;
  external_tenant_id: string;
  name: string;
  status: TenantStatus;
  quota?: TenantQuota;
  created_at: string;
}

export interface TenantQuota {
  max_events_per_day: number;
  max_concurrent_queries: number;
}

// ─── Agents ──────────────────────────────────────────────────────────────────

export type AgentStatus = 'connected' | 'disconnected' | 'error';

export interface Agent {
  id: string;
  application_id: string;
  name: string;
  status: AgentStatus;
  last_seen_at?: string;
  created_at: string;
}

export type TokenStatus = 'active' | 'expired' | 'revoked';

/** Credential metadata. The secret is only ever returned once, at issue/rotate time. */
export interface AgentToken {
  id: string;
  agent_id: string;
  name?: string;
  token_prefix: string;
  status: TokenStatus;
  created_at: string;
  expires_at?: string;
  revoked_at?: string;
}

export interface IssuedToken {
  token_id: string;
  token: string;
  token_prefix: string;
  expires_at?: string;
  previous_token_id?: string;
  previous_token_expires_at?: string;
}

export interface TenantMapping {
  id: string;
  agent_id: string;
  external_tenant_id: string;
  tenant_id: string;
  created_at: string;
}

// ─── Analytics ───────────────────────────────────────────────────────────────

export interface OrderSummary {
  tenant_id: string;
  from: string;
  to: string;
  total_orders: number;
  total_revenue: number;
  currency: string;
  cancelled_orders: number;
  active_orders: number;
  last_processed_at: string;
}

export interface TrendPoint {
  bucket: string; // ISO timestamp
  order_count: number;
  revenue: number;
}

export interface TrendResponse {
  tenant_id: string;
  interval: 'hour' | 'day';
  data: TrendPoint[];
}

export interface TopProduct {
  product_id: string;
  product_name: string;
  category: string;
  order_count: number;
  revenue: number;
}

export interface TopProductsResponse {
  tenant_id: string;
  data: TopProduct[];
}

export interface BreakdownItem {
  group: string;
  count: number;
  revenue: number;
}

export interface BreakdownResponse {
  tenant_id: string;
  group_by: string;
  data: BreakdownItem[];
}

export interface AnalyticsStatus {
  tenant_id: string;
  last_processed_at: string;
  last_lsn: string;
  lag_seconds: number;
  is_healthy: boolean;
}

export interface DeadLetterEvent {
  id: number;
  agent_id: string;
  tenant_id: string;
  event_id: string;
  lsn: string;
  reason: string;
  payload?: unknown;
  retry_count: number;
  replayed_at?: string;
  created_at: string;
}

// ─── Access & Policies ───────────────────────────────────────────────────────

export type PolicyType = 'rate_limit' | 'query_limit' | 'data_access';

export interface Policy {
  id: string;
  tenant_id: string;
  name: string;
  type: PolicyType;
  config: Record<string, unknown>;
  created_at: string;
}

// ─── Shared ───────────────────────────────────────────────────────────────────

export interface ApiError {
  code: string;
  message: string;
  details?: unknown;
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  per_page: number;
}


// ─── Users & roles ───────────────────────────────────────────────────────────

export type Role = 'admin' | 'operator';

export interface User {
  id: string;
  username: string;
  role: Role;
  /** Absent for platform operators. */
  organization_id?: string;
}

// ─── Metric configuration ────────────────────────────────────────────────────

export type Aggregation = 'count' | 'sum' | 'avg' | 'min' | 'max' | 'count_distinct';

export interface MetricFilter {
  field: string;
  op: 'eq' | 'neq' | 'in';
  value: string | string[];
}

export interface MetricConfig {
  source: 'orders';
  aggregation: Aggregation;
  field?: string;
  group_by?: string[];
  filters?: MetricFilter[];
  granularity?: Array<'hour' | 'day'>;
}

export interface MetricDefinition {
  id: string;
  /** null for built-in definitions. */
  organization_id: string | null;
  name: string;
  description?: string;
  config: MetricConfig;
  enabled: boolean;
  builtin: boolean;
  created_at: string;
  updated_at: string;
}

// ─── Audit ───────────────────────────────────────────────────────────────────

export interface AuditLog {
  id: number;
  organization_id?: string;
  actor_id: string;
  actor_name: string;
  actor_role: Role;
  action: string;
  resource_type: string;
  resource_id: string;
  details: Record<string, unknown>;
  created_at: string;
}

export interface AuditLogPage {
  data: AuditLog[];
  next_before_id?: number;
}

// ─── Monitoring (B-5) ─────────────────────────────────────────────────────────

export interface MonitoringStats {
  queue_depth: number;
  events_last_60s: number;
  events_last_5m: number;
  last_processed_at?: string;
  worker_healthy: boolean;
  dead_letter_count: number;
  processed_last_5m: number;
  latency_p50_ms?: number;
  latency_p95_ms?: number;
  latency_p99_ms?: number;
  latency_sample_count: number;
}

