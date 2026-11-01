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
  region: TenantRegion;
  plan_tier: TenantPlan;
  billing_cycle: BillingCycle;
  tags: string[];
  metadata: Record<string, unknown>;
  created_at: string;
}

export type TenantRegion = 'apac' | 'emea' | 'americas' | 'global';
export type TenantPlan = 'free' | 'growth' | 'enterprise';
export type BillingCycle = 'monthly' | 'annual';

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
  discount: number;
  net_revenue: number;
  new_customers: number;
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
  /** Same value as total_revenue; kept for older clients. */
  revenue: number;
  total_revenue: number;
  quantity_sold: number;
  total_discount: number;
  return_count: number;
  return_rate_pct: number;
  avg_unit_price: number;
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
  last_processed_at: string | null;
  last_lsn: string;
  lag_seconds: number | null;
  is_healthy: boolean;
  activity_state: 'waiting' | 'idle' | 'active' | 'delayed';
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
  worker_state: 'unknown' | 'idle' | 'processing' | 'stalled';
  dead_letter_count: number;
  processed_last_5m: number;
  latency_p50_ms?: number;
  latency_p95_ms?: number;
  latency_p99_ms?: number;
  latency_sample_count: number;
}


// ─── Schema expansion: analytics responses ───────────────────────────────────

export type BreakdownGroup =
  | 'status' | 'category' | 'payment_method' | 'channel' | 'country'
  | 'customer_tier' | 'fulfillment_status' | 'return_reason' | 'referrer_source';

export interface RevenueBreakdown {
  tenant_id: string;
  from: string;
  to: string;
  currency: string;
  gross_revenue: number;
  total_discount: number;
  net_revenue: number;
  total_tax: number;
  total_shipping: number;
  active_orders: number;
  avg_order_value: number;
  total_quantity: number;
}

export interface CustomerSummary {
  tenant_id: string;
  total_customers: number;
  new_customers: number;
  returning_customers: number;
  new_customer_rate_pct: number;
  avg_ltv: number;
  avg_order_frequency: number;
  avg_orders_per_customer: number;
}

export interface TopBuyer {
  customer_id: string;
  customer_tier: string;
  order_count: number;
  total_spend: number;
  total_discount: number;
  avg_order_value: number;
  first_order_at?: string;
  last_order_at?: string;
}

export interface CohortPoint {
  bucket: string; // YYYY-MM-DD
  new_orders: number;
  returning_orders: number;
  new_revenue: number;
  returning_revenue: number;
}

export type GeoGroup = 'country' | 'state' | 'city';

export interface GeoItem {
  group: string;
  country?: string;
  state_province?: string;
  order_count: number;
  total_revenue: number;
  share_pct: number;
}

export interface ChannelItem {
  channel: string;
  referrer_source: string;
  order_count: number;
  total_revenue: number;
  new_customers: number;
}

export interface PaymentItem {
  payment_method: string;
  total_orders: number;
  paid_orders: number;
  pending_orders: number;
  failed_orders: number;
  refunded_orders: number;
  chargeback_orders: number;
  total_amount: number;
  failure_rate_pct: number;
}

export interface ReturnsSummary {
  tenant_id: string;
  total_returns: number;
  total_refund_amount: number;
  return_rate_pct: number;
  top_reasons: Array<{ reason: string; count: number; share_pct: number }>;
}

export interface FunnelStage {
  stage: string;
  order_count: number;
  pct_of_total: number;
}

export interface DiscountSummary {
  tenant_id: string;
  orders_with_discount: number;
  discount_rate_pct: number;
  total_discount_given: number;
  avg_discount_per_order: number;
  top_codes: Array<{ code: string; uses: number; total_discount: number }>;
}

export interface TenantAlert {
  code: string;
  severity: 'warning' | 'error';
  message: string;
}

export interface TenantHealth {
  activity_state: 'waiting' | 'idle' | 'active' | 'delayed';
  tenant_id: string;
  last_event_at?: string;
  lag_seconds?: number;
  dead_letter_count: number;
  revenue_24h: number;
  orders_24h: number;
  payment_failure_rate_pct: number;
  return_rate_pct: number;
  alerts: TenantAlert[];
}

// ─── Schema expansion: monitoring ────────────────────────────────────────────

export interface IngestionPoint {
  minute: string;
  events: number;
}

export interface MonitoringDetail {
  tenant_id: string;
  event_types_5m: Array<{ event_type: string; count: number }>;
  schema_versions_24h: Array<{ schema_version: number; events: number; first_seen: string; last_seen: string }>;
  dead_letter_by_tenant: Array<{ tenant_id: string; unresolved: number; latest_reason: string; latest_failure_at: string }>;
  agents: Array<{
    agent_id: string;
    agent_name: string;
    application: string;
    confirmed_lsn: string;
    last_lsn: string;
    last_seen_at?: string;
    last_processed_at?: string;
    lag_seconds?: number;
    status: AgentStatus;
  }>;
}
