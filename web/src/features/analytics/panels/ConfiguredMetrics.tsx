import { analytics, metricDefinitions } from '../../../lib/api';
import type { MetricDefinition } from '../../../types';
import type { PanelScope } from './format';
import { compactMetricValue, metricRows, metricValue, visibleMetrics } from './metricResults';
import { Empty, Panel } from './shared';
import { usePanel } from './usePanel';

export function ConfiguredMetrics({ scope, organizationId }: { scope: PanelScope; organizationId?: string }) {
  const definitions = usePanel(
    () => metricDefinitions.list(organizationId).then((response) => response.data),
    `metric-definitions|${organizationId ?? ''}`,
  );
  const metrics = visibleMetrics(definitions.data ?? [], organizationId);

  return (
    <section aria-label="Configured metrics">
      <h2 className="section-title">Configured metrics</h2>
      <p className="hint">Enabled metrics from Metric Configuration, calculated for the selected scope and date range. Refreshes every 3 seconds.</p>
      {definitions.loading || definitions.error ? (
        <Panel title="Metric definitions" loading={definitions.loading} error={definitions.error} onRetry={definitions.reload}>
          {definitions.error && <p className="hint">Metric definitions could not refresh.</p>}
        </Panel>
      ) : null}
      {!definitions.loading && !definitions.error && metrics.length === 0 && <Empty>No enabled metrics. Add or enable one in Metric Configuration.</Empty>}
      <div className="panel-grid">
        {metrics.map((metric) => <ConfiguredMetric key={`${metric.organization_id ?? 'builtin'}:${metric.id}`} metric={metric} scope={scope} />)}
      </div>
    </section>
  );
}

function ConfiguredMetric({ metric, scope }: { metric: MetricDefinition; scope: PanelScope }) {
  const data = usePanel(
    () => analytics.metric(metric.id, { from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((response) => response.data),
    JSON.stringify(['configured-metric', scope, metric.organization_id, metric.id, metric.updated_at, metric.config]),
  );
  const rows = metricRows(data.data?.result ?? null);
  const groups = metric.config.group_by ?? [];
  // Organization product groups also identify the tenant owning each product ID.
  const dimensions = rows.some((row) => 'tenant_id' in row) ? ['tenant_id', ...groups] : groups;
  const valueColumn = rows[0] && Object.keys(rows[0]).find((key) => !dimensions.includes(key));
  const label = (field: string) => field.replaceAll('_', ' ');
  const definition = metric.config.aggregation === 'count' ? 'Count of orders'
    : `${label(metric.config.aggregation)} of ${label(metric.config.field ?? 'amount')}`;

  return (
    <Panel title={metric.name} loading={data.loading} error={data.error} onRetry={data.reload}>
      {metric.description && <p className="hint">{metric.description}</p>}
      <p className="hint">{definition}{groups.length > 0 ? ` by ${groups.map(label).join(', ')}` : ''}</p>
      {!data.loading && data.data && (
        rows.length === 0 ? <Empty /> : groups.length === 0 ? (
          <div className="stat-value" title={metricValue(valueColumn ? rows[0][valueColumn] : null)}>
            {compactMetricValue(valueColumn ? rows[0][valueColumn] : null)}
          </div>
        ) : (
          <div style={{ overflow: 'auto', maxHeight: 320 }}>
            <table className="table">
              <thead><tr>{dimensions.map((field) => <th key={field}>{label(field)}</th>)}<th>Value</th></tr></thead>
              <tbody>{rows.map((row) => (
                <tr key={JSON.stringify(dimensions.map((field) => row[field]))}>
                  {dimensions.map((field) => <td key={field}>{metricValue(row[field])}</td>)}
                  <td className="mono" title={metricValue(valueColumn ? row[valueColumn] : null)}>
                    {compactMetricValue(valueColumn ? row[valueColumn] : null)}
                  </td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )
      )}
    </Panel>
  );
}
