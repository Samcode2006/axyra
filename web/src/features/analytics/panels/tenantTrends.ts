import type { Tenant, TrendPoint } from '../../../types';

export type TrendMetric = 'revenue' | 'net_revenue' | 'discount' | 'new_customers' | 'order_count';

const COLORS = ['#3b82f6', '#f97316', '#10b981', '#a855f7', '#ec4899', '#06b6d4', '#eab308', '#ef4444', '#84cc16', '#6366f1'];

export function tenantTrendOptions(tenants: Pick<Tenant, 'id' | 'name'>[], points: TrendPoint[]) {
  const names = new Map(tenants.map((tenant) => [tenant.id, tenant.name]));
  // Include every returned tenant, even when the tenant list is paginated.
  for (const point of points) if (point.tenant_id && !names.has(point.tenant_id)) names.set(point.tenant_id, point.tenant_id);
  return [...names].sort(([a], [b]) => a.localeCompare(b)).map(([id, name], index) => ({
    id, name, color: COLORS[index] ?? `hsl(${(index * 137.508) % 360} 65% 50%)`,
  }));
}

export function tenantTrendLines(options: ReturnType<typeof tenantTrendOptions>, points: TrendPoint[], metric: TrendMetric, selected: string) {
  const buckets = [...new Set(points.map((point) => point.bucket))].sort((a, b) => Date.parse(a) - Date.parse(b));
  const values = new Map<string, Map<string, number>>();
  for (const point of points) {
    if (!point.tenant_id) continue;
    if (!values.has(point.tenant_id)) values.set(point.tenant_id, new Map());
    values.get(point.tenant_id)!.set(point.bucket, point[metric]);
  }
  return options.filter((option) => (!selected || option.id === selected) && values.has(option.id)).map((option) => ({
    ...option,
    points: buckets.map((bucket) => ({ bucket, value: values.get(option.id)?.get(bucket) ?? 0 })),
  }));
}
