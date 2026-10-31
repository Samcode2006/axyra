import axios, { AxiosError, type AxiosInstance } from 'axios';
import type {
  Agent,
  AgentToken,
  AnalyticsStatus,
  Application,
  AuditLogPage,
  BreakdownResponse,
  IssuedToken,
  MetricConfig,
  MetricDefinition,
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

export const tenants = {
  list: (params?: { page?: number; per_page?: number; status?: string }) =>
    http.get<PaginatedResponse<Tenant>>('/v1/tenants', { params }),
  create: (payload: {
    external_tenant_id: string;
    name: string;
    organization_id?: string;
    quota?: Partial<TenantQuota>;
  }) => http.post<Tenant>('/v1/tenants', payload),
  update: (
    id: string,
    payload: { name?: string; status?: Tenant['status']; quota?: Partial<TenantQuota> }
  ) => http.patch<Tenant>(`/v1/tenants/${id}`, payload),
  remove: (id: string) => http.delete(`/v1/tenants/${id}`),
};

// ─── Analytics ───────────────────────────────────────────────────────────────

export const analytics = {
  summary: (params: { from: string; to: string }) =>
    http.get<OrderSummary>('/v1/analytics/orders/summary', { params }),

  trend: (params: { interval: 'hour' | 'day'; from: string; to: string }) =>
    http.get<TrendResponse>('/v1/analytics/orders/trend', { params }),

  topProducts: (params: { limit?: number; from: string; to: string }) =>
    http.get<TopProductsResponse>('/v1/analytics/products/top', { params }),

  breakdown: (params: { group_by: 'status' | 'category'; from: string; to: string }) =>
    http.get<BreakdownResponse>('/v1/analytics/orders/breakdown', { params }),

  status: () => http.get<AnalyticsStatus>('/v1/analytics/status'),
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
  list: () => http.get<MetricDefinition[]>('/v1/metric-definitions'),
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
