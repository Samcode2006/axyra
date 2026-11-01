import axios, { AxiosError, type AxiosInstance } from 'axios';
import type {
  Agent,
  AgentToken,
  AnalyticsStatus,
  Application,
  AuditLogPage,
  BreakdownGroup,
  BreakdownResponse,
  ChannelItem,
  CohortPoint,
  CustomerSummary,
  DiscountSummary,
  FunnelStage,
  GeoGroup,
  GeoItem,
  IngestionPoint,
  MonitoringDetail,
  PaymentItem,
  ReturnsSummary,
  RevenueBreakdown,
  TenantHealth,
  TopBuyer,
  DeadLetterEvent,
  IssuedToken,
  MetricConfig,
  MetricDefinition,
  MetricQueryResponse,
  MonitoringStats,
  OrderSummary,
  Organization,
  PaginatedResponse,
  Policy,
  Tenant,
  TenantMapping,
  TenantQuota,
  TopProductsResponse,
  TrendResponse,
  User,
} from '../types';

// ─── Base client ─────────────────────────────────────────────────────────────

// The control plane listens on 8081 (see deploy/docker-compose.yaml).
const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8081';

export const http: AxiosInstance = axios.create({
  baseURL: BASE_URL,
  headers: { 'Content-Type': 'application/json' },
  timeout: 15_000,
});

const TOKEN_KEY = 'axyra_token';
const USER_KEY = 'axyra_user';

// Attach auth token from sessionStorage on every request
http.interceptors.request.use((config) => {
  const token = sessionStorage.getItem(TOKEN_KEY);
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// An expired/invalid session sends the user back to login. A failed login also
// returns 401, and must stay on the page so its error message can be shown.
http.interceptors.response.use(
  (res) => res,
  (err: AxiosError) => {
    const isLogin = err.config?.url?.includes('/v1/auth/login');
    if (err.response?.status === 401 && !isLogin) {
      clearSession();
      window.location.href = '/login';
    }
    return Promise.reject(err);
  }
);

/** Extracts the server's message from an API error, with a fallback. */
export function errMsg(err: unknown, fallback = 'Something went wrong'): string {
  const e = err as AxiosError<{ error?: { message?: string } }>;
  return e.response?.data?.error?.message ?? (e.response ? fallback : 'Cannot reach the Axyra API');
}

function clearSession() {
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(USER_KEY);
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

export const auth = {
  login: async (username: string, password: string) => {
    const res = await http.post<{ token: string; user: User }>('/v1/auth/login', { username, password });
    sessionStorage.setItem(TOKEN_KEY, res.data.token);
    sessionStorage.setItem(USER_KEY, JSON.stringify(res.data.user));
    return res.data.user;
  },

  logout: () => clearSession(),

  isAuthenticated: () => !!sessionStorage.getItem(TOKEN_KEY),

  /** The signed-in user. Used only to adapt the UI; the server enforces access. */
  user: (): User | null => {
    try {
      return JSON.parse(sessionStorage.getItem(USER_KEY) ?? 'null') as User | null;
    } catch {
      return null;
    }
  },

  isOperator: () => auth.user()?.role === 'operator',
};

// ─── Organizations ────────────────────────────────────────────────────────────

export const organizations = {
  list: () => http.get<Organization[]>('/v1/organizations'),
  create: (payload: { name: string }) => http.post<Organization>('/v1/organizations', payload),
  update: (id: string, payload: { name: string }) =>
    http.patch<Organization>(`/v1/organizations/${id}`, payload),
  remove: (id: string) => http.delete(`/v1/organizations/${id}`),
  users: (id: string) => http.get<User[]>(`/v1/organizations/${id}/users`),
  createUser: (id: string, payload: { username: string; password: string }) =>
    http.post<User>(`/v1/organizations/${id}/users`, payload),
  removeUser: (id: string, userId: string) => http.delete(`/v1/organizations/${id}/users/${userId}`),
};

// ─── Applications ────────────────────────────────────────────────────────────

export const applications = {
  list: () => http.get<Application[]>('/v1/applications'),
  create: (payload: { name: string; description?: string; organization_id?: string }) =>
    http.post<Application>('/v1/applications', payload),
  update: (id: string, payload: { name?: string; description?: string }) =>
    http.patch<Application>(`/v1/applications/${id}`, payload),
  remove: (id: string) => http.delete(`/v1/applications/${id}`),
};

// ─── Agents ──────────────────────────────────────────────────────────────────

export const agents = {
  list: () => http.get<Agent[]>('/v1/agents'),
  create: (payload: { application_id: string; name: string }) => http.post<Agent>('/v1/agents', payload),
  update: (id: string, payload: { name: string }) => http.patch<Agent>(`/v1/agents/${id}`, payload),
  remove: (id: string) => http.delete(`/v1/agents/${id}`),

  tokens: (agentId: string) => http.get<AgentToken[]>(`/v1/agents/${agentId}/tokens`),
  issueToken: (agentId: string, payload: { name?: string; expires_in_days?: number } = {}) =>
    http.post<IssuedToken>(`/v1/agents/${agentId}/tokens`, payload),
  rotateToken: (
    agentId: string,
    tokenId: string,
    payload: { grace_period_seconds?: number; expires_in_days?: number } = {}
  ) => http.post<IssuedToken>(`/v1/agents/${agentId}/tokens/${tokenId}/rotate`, payload),
  revokeToken: (agentId: string, tokenId: string) =>
    http.delete(`/v1/agents/${agentId}/tokens/${tokenId}`),

  mappings: (agentId: string) => http.get<TenantMapping[]>(`/v1/agents/${agentId}/tenant-mappings`),
  createMapping: (agentId: string, payload: { external_tenant_id: string; tenant_id: string }) =>
    http.post<TenantMapping>(`/v1/agents/${agentId}/tenant-mappings`, payload),
  removeMapping: (agentId: string, mappingId: string) =>
    http.delete(`/v1/agents/${agentId}/tenant-mappings/${mappingId}`),
};

// ─── Tenants ─────────────────────────────────────────────────────────────────

export interface ProvisioningStatus {
 status: 'external' | 'queued' | 'provisioning' | 'ready' | 'failed';
 stage?: string; error?: string; host?: string; database?: string; port?: number; agent_id?: string;
}
export const tenants = {
 provisioning: (id: string) => http.get<ProvisioningStatus>(`/v1/tenants/${id}/provisioning`),
 retryProvisioning: (id: string) => http.post(`/v1/tenants/${id}/provisioning/retry`),
  list: (params?: { page?: number; per_page?: number; status?: string }) =>
    http.get<PaginatedResponse<Tenant>>('/v1/tenants', { params }),
  create: (payload: {
    database_mode?: 'existing' | 'managed';
    external_tenant_id: string;
    name: string;
    organization_id?: string;
    quota?: Partial<TenantQuota>;
    region?: Tenant['region'];
    plan_tier?: Tenant['plan_tier'];
    billing_cycle?: Tenant['billing_cycle'];
    tags?: string[];
  }) => http.post<Tenant>('/v1/tenants', payload),
  update: (
    id: string,
    payload: {
      name?: string;
      status?: Tenant['status'];
      quota?: Partial<TenantQuota>;
      region?: Tenant['region'];
      plan_tier?: Tenant['plan_tier'];
      billing_cycle?: Tenant['billing_cycle'];
      tags?: string[];
    }
  ) => http.patch<Tenant>(`/v1/tenants/${id}`, payload),
  remove: (id: string) => http.delete(`/v1/tenants/${id}`),
};

// ─── Analytics ───────────────────────────────────────────────────────────────

export const ORGANIZATION_SCOPE = '__organization__';
function dashboardParams<T extends { tenant_id?: string }>(params?: T) {
 return params?.tenant_id === ORGANIZATION_SCOPE ? { ...params, tenant_id: undefined, scope: 'organization' } : params;
}

export const analytics = {
  metric: (id: string, params: { from: string; to: string; tenant_id?: string }) =>
    http.get<MetricQueryResponse>(`/v1/analytics/metrics/${encodeURIComponent(id)}`, { params: dashboardParams(params) }),
  summary: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<OrderSummary>('/v1/analytics/orders/summary', { params: dashboardParams(params) }),

  trend: (params: { interval: 'hour' | 'day'; from: string; to: string; tenant_id?: string; group_by?: 'tenant' }) =>
    http.get<TrendResponse>('/v1/analytics/orders/trend', { params: dashboardParams(params) }),

  topProducts: (params: {
    limit?: number;
    from: string;
    to: string;
    sort_by?: 'revenue' | 'quantity' | 'returns';
    tenant_id?: string;
  }) => http.get<TopProductsResponse>('/v1/analytics/products/top', { params: dashboardParams(params) }),

  breakdown: (params: { group_by: BreakdownGroup; from: string; to: string; tenant_id?: string }) =>
    http.get<BreakdownResponse>('/v1/analytics/orders/breakdown', { params: dashboardParams(params) }),

  // Schema expansion endpoints. Every one is tenant-scoped by the control plane
  // for customer admins; operators may pass tenant_id.
  revenue: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<RevenueBreakdown>('/v1/analytics/orders/revenue', { params: dashboardParams(params) }),
  customerSummary: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<CustomerSummary>('/v1/analytics/customers/summary', { params: dashboardParams(params) }),
  topBuyers: (params: { limit?: number; from?: string; to?: string; tenant_id?: string }) =>
    http.get<{ tenant_id: string; data: TopBuyer[] }>('/v1/analytics/customers/top', { params: dashboardParams(params) }),
  cohorts: (params: { from: string; to: string; interval: 'day' | 'week'; tenant_id?: string }) =>
    http.get<{ tenant_id: string; interval: string; data: CohortPoint[] }>('/v1/analytics/customers/cohorts', { params: dashboardParams(params) }),
  geo: (params: { from: string; to: string; group_by: GeoGroup; limit?: number; tenant_id?: string }) =>
    http.get<{ tenant_id: string; group_by: string; data: GeoItem[] }>('/v1/analytics/geo/breakdown', { params: dashboardParams(params) }),
  channels: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<{ tenant_id: string; data: ChannelItem[] }>('/v1/analytics/channels/breakdown', { params: dashboardParams(params) }),
  payments: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<{ tenant_id: string; data: PaymentItem[] }>('/v1/analytics/payments/breakdown', { params: dashboardParams(params) }),
  returns: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<ReturnsSummary>('/v1/analytics/returns/summary', { params: dashboardParams(params) }),
  funnel: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<{ tenant_id: string; data: FunnelStage[] }>('/v1/analytics/fulfillment/funnel', { params: dashboardParams(params) }),
  discounts: (params: { from: string; to: string; tenant_id?: string }) =>
    http.get<DiscountSummary>('/v1/analytics/discounts/summary', { params: dashboardParams(params) }),
  tenantHealth: (params: { tenant_id: string }) =>
    http.get<TenantHealth>('/v1/analytics/tenant/health', { params }),

  status: (params?: { tenant_id?: string }) =>
    http.get<AnalyticsStatus>('/v1/analytics/status', { params: dashboardParams(params) }),

  deadLetter: (params?: { agent_id?: string; limit?: number }) =>
    http.get<DeadLetterEvent[]>('/v1/analytics/dead-letter', { params }),

  monitoring: () =>
    http.get<MonitoringStats>('/v1/analytics/monitoring'),
  monitoringDetail: () =>
    http.get<MonitoringDetail>('/v1/analytics/monitoring/detail'),
  ingestionRate: () =>
    http.get<{ data: IngestionPoint[]; total_last_hour: number }>('/v1/analytics/monitoring/ingestion-rate'),

  rebuild: (params?: { tenant_id?: string }) =>
    http.post<{ tenant_id: string; events_replayed: number; duration_ms: number }>(
      '/v1/analytics/rebuild',
      null,
      { params }
    ),
};

// ─── Policies ────────────────────────────────────────────────────────────────

export const policies = {
  list: () => http.get<Policy[]>('/v1/policies'),
  create: (payload: Pick<Policy, 'tenant_id' | 'name' | 'type' | 'config'>) =>
    http.post<Policy>('/v1/policies', payload),
  update: (id: string, payload: { name?: string; config?: Record<string, unknown> }) =>
    http.patch<Policy>(`/v1/policies/${id}`, payload),
  delete: (id: string) => http.delete(`/v1/policies/${id}`),
};

// ─── Metric definitions ──────────────────────────────────────────────────────

export const metricDefinitions = {
  list: (organizationId?: string) => http.get<MetricDefinition[]>('/v1/metric-definitions', {
    params: organizationId ? { organization_id: organizationId } : undefined,
  }),
  put: (
    id: string,
    payload: {
      name: string;
      description?: string;
      config: MetricConfig;
      enabled?: boolean;
      organization_id?: string;
    }
  ) => http.put<MetricDefinition>(`/v1/metric-definitions/${id}`, payload),
  remove: (id: string, organizationId?: string) =>
    http.delete(`/v1/metric-definitions/${id}`, {
      params: organizationId ? { organization_id: organizationId } : undefined,
    }),
};

// ─── Audit log ───────────────────────────────────────────────────────────────

export const auditLogs = {
  list: (params?: {
    limit?: number;
    before_id?: number;
    resource_type?: string;
    resource_id?: string;
    action?: string;
  }) => http.get<AuditLogPage>('/v1/audit-logs', { params }),
};
