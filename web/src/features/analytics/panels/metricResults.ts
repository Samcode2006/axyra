import type { MetricDefinition, MetricQueryResponse } from '../../../types';
import { formatCompactNumber } from '../../../lib/utils.ts';

/** Resolve scope before enabled status, so a disabled override cannot fall back to a built-in. */
export function visibleMetrics(definitions: MetricDefinition[], organizationId?: string): MetricDefinition[] {
  const effective = new Map<string, MetricDefinition>();
  for (const metric of definitions) {
    if (metric.organization_id && metric.organization_id !== organizationId) continue;
    if (!effective.has(metric.id) || metric.organization_id) effective.set(metric.id, metric);
  }
  return [...effective.values()].filter((metric) => metric.enabled)
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
}

export function metricRows(result: MetricQueryResponse['result']) {
  return result === null ? [] : Array.isArray(result) ? result : [result];
}

export function metricValue(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return '—';
  // PostgreSQL NUMERIC arrives as a string. Keep its precision and decimal places.
  if (typeof value === 'string') return value || '(empty)';
  return value.toLocaleString(undefined, { maximumFractionDigits: 20 });
}

/** Compact aggregate values only; dimension labels and full-value tooltips stay exact. */
export function compactMetricValue(value: string | number | null | undefined): string {
  const numeric = typeof value === 'string' ? Number(value) : value;
  if (numeric == null || !Number.isFinite(numeric) || Math.abs(numeric) < 1000) return metricValue(value);
  return formatCompactNumber(numeric).replace('K', 'k');
}
