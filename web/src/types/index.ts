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

// ─── Access & Policies ───────────────────────────────────────────────────────

export interface Policy {
  id: string;
  tenant_id: string;
  name: string;
  type: 'rate_limit' | 'query_limit' | 'data_access';
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

