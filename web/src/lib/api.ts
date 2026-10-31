import axios, { AxiosError, type AxiosInstance } from 'axios';
import type {
  Agent,
  AnalyticsStatus,
  Application,
  BreakdownResponse,
  OrderSummary,
  Organization,
  PaginatedResponse,
  Policy,
  Tenant,
  TopProductsResponse,
  TrendResponse,
} from '../types';

// ─── Base client ─────────────────────────────────────────────────────────────

const BASE_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080';

export const http: AxiosInstance = axios.create({
  baseURL: BASE_URL,
  headers: { 'Content-Type': 'application/json' },
  timeout: 15_000,
});

// Attach auth token from sessionStorage on every request
http.interceptors.request.use((config) => {
  const token = sessionStorage.getItem('axyra_token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// Normalize errors
http.interceptors.response.use(
  (res) => res,
  (err: AxiosError) => {
    if (err.response?.status === 401) {
      sessionStorage.removeItem('axyra_token');
      window.location.href = '/login';
    }
    return Promise.reject(err);
  }
);

// ─── Auth ─────────────────────────────────────────────────────────────────────

export const auth = {
  login: (username: string, password: string) =>
    http.post<{ token: string }>('/v1/auth/login', { username, password }),

  logout: () => {
    sessionStorage.removeItem('axyra_token');
  },

  isAuthenticated: () => !!sessionStorage.getItem('axyra_token'),
};

// ─── Organizations ────────────────────────────────────────────────────────────

export const organizations = {
  list: () => http.get<Organization[]>('/v1/organizations'),
  create: (payload: { name: string }) =>
    http.post<Organization>('/v1/organizations', payload),
};

// ─── Applications ────────────────────────────────────────────────────────────

export const applications = {
  list: () => http.get<Application[]>('/v1/applications'),
  create: (payload: { name: string; description?: string; organization_id: string }) =>
    http.post<Application>('/v1/applications', payload),
};

// ─── Agents ──────────────────────────────────────────────────────────────────

export const agents = {
  list: () => http.get<Agent[]>('/v1/agents'),
  issueToken: (agentId: string) =>
    http.post<{ token_id: string; token: string }>(
      `/v1/agents/${agentId}/tokens`
    ),
  revokeToken: (agentId: string, tokenId: string) =>
    http.delete(`/v1/agents/${agentId}/tokens/${tokenId}`),
};

// ─── Tenants ─────────────────────────────────────────────────────────────────

export const tenants = {
  list: () => http.get<PaginatedResponse<Tenant>>('/v1/tenants'),
  create: (payload: {
    external_tenant_id: string;
    name: string;
    organization_id: string;
  }) => http.post<Tenant>('/v1/tenants', payload),
  update: (id: string, payload: Partial<Pick<Tenant, 'status' | 'quota'>>) =>
    http.patch<Tenant>(`/v1/tenants/${id}`, payload),
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
  create: (payload: Omit<Policy, 'id' | 'created_at'>) =>
    http.post<Policy>('/v1/policies', payload),
  delete: (id: string) => http.delete(`/v1/policies/${id}`),
};

