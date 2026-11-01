import { http } from '../../lib/api';

export interface Source { id: string; name: string; store: string; configured: boolean }
export interface Product { product_id: string; order_count: number; revenue: number }
export interface Sample {
  source: Product[]; analytics: Product[]; match: boolean; source_stable: boolean;
  differences: string[]; source_ms: number; analytics_ms: number; checked_at: string;
}
export interface Ticket { ticket: string; prefix: string; from: string; to: string; step: number }
export interface Window { from?: string; to?: string }
export const comparison = {
  sources: (signal: AbortSignal) => http.get<Source[]>('/v1/comparison/sources', { signal }),
  snapshot: (tenant: string, window: Window, signal: AbortSignal) =>
    http.get<Sample>('/v1/comparison/snapshot', { params: { tenant_id: tenant, ...window }, signal }),
  step: (tenant: string, step: number, ticket: string, signal: AbortSignal) =>
    http.post<Ticket>('/v1/comparison/simulation', { tenant_id: tenant, step, ticket }, { signal }),
};
