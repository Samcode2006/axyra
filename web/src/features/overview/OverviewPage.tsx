import { DBQueryTime } from '../../components/DBQueryTime';
import { useDBQueryTiming } from '../../lib/useDBQueryTiming';
import AnalyticsPage from '../analytics/AnalyticsPage';
import { useRef, useState } from 'react';
import { StatCard } from '../../components/StatCard';
import { Badge } from '../../components/Badge';
import { Spinner, ErrorMsg } from '../../components/Feedback';
import { analytics, tenants, agents, auth } from '../../lib/api';
import { usePanel } from '../analytics/panels/usePanel';
import { ConfiguredMetrics } from '../analytics/panels/ConfiguredMetrics';
import { formatCompactCurrency, formatCompactNumber, formatCurrency, formatNumber, relativeTime, utcDayRange } from '../../lib/utils';
import type { CustomerSummary, RevenueBreakdown, Tenant, TenantAlert, TenantHealth } from '../../types';

/** The analytics API is per tenant, so organization-wide figures fan out. Capped to bound the request count. */
const MAX_TENANTS = 12;

interface TenantRow {
  refreshFailed?: boolean;
  tenant: Tenant;
  health?: TenantHealth;
  revenue?: RevenueBreakdown;
  customers?: CustomerSummary;
}

const ok = <T,>(r: PromiseSettledResult<{ data: T }>): T | undefined => (r.status === 'fulfilled' ? r.value.data : undefined);

export default function OverviewPage() {
 return auth.isOperator() ? <OperatorOverview /> : <AnalyticsPage organization />;
}

function OperatorOverview() {
  const timings = useDBQueryTiming('overview', 'revenue');
  const previous = useRef(new Map<string, TenantRow>());
  const live = usePanel(async () => {
      const [t, a] = await Promise.all([tenants.list({ per_page: 200 }), agents.list()]);
      const all = t.data.data ?? [];

      const { from, to } = utcDayRange(7);
      const shown = all.filter((x) => x.status === 'active').slice(0, MAX_TENANTS);
      // allSettled: one tenant failing (or analytics being down) must not hide the rest.
      const detail = await Promise.all(
        shown.map(async (tenant): Promise<TenantRow> => {
          const [h, r, c] = await Promise.allSettled([
            analytics.tenantHealth({ tenant_id: tenant.id }),
            analytics.revenue({ from, to, tenant_id: tenant.id }),
            analytics.customerSummary({ from, to, tenant_id: tenant.id }),
          ]);
          const cached = previous.current.get(tenant.id);
          const row = { tenant, health: ok(h) ?? cached?.health, revenue: ok(r) ?? cached?.revenue, customers: ok(c) ?? cached?.customers };
          previous.current.set(tenant.id, row);
          return { ...row, refreshFailed: [h, r, c].some((result) => result.status === 'rejected') };
        })
      );
      // Tenants that are not active still belong in the health table.
      const inactive = all.filter((x) => x.status !== 'active').map((tenant): TenantRow => ({ tenant }));
      return { rows: [...detail, ...inactive], tenantTotal: t.data.total, agentList: a.data, tenantList: all };
  }, 'overview', 3000);

  if (live.loading) return <Spinner />;
  if (!live.data) return <ErrorMsg message={live.error ?? 'Could not load overview'} />;
  const { rows, tenantTotal, agentList } = live.data;

  const withData = rows.filter((r) => r.revenue);
  const gross = withData.reduce((s, r) => s + (r.revenue?.gross_revenue ?? 0), 0);
  const net = withData.reduce((s, r) => s + (r.revenue?.net_revenue ?? 0), 0);
  const orders = withData.reduce((s, r) => s + (r.revenue?.active_orders ?? 0), 0);
  const newCustomers = rows.reduce((s, r) => s + (r.customers?.new_customers ?? 0), 0);
  const activeTenants = rows.filter((r) => r.tenant.status === 'active').length;
  const connectedAgents = agentList.filter((a) => a.status === 'connected').length;

  const alerts: Array<TenantAlert & { tenantName: string }> = rows.flatMap((r) =>
    (r.health?.alerts ?? []).map((a) => ({ ...a, tenantName: r.tenant.name }))
  );
  const top = [...rows]
    .filter((r) => (r.health?.revenue_24h ?? 0) > 0)
    .sort((a, b) => (b.health?.revenue_24h ?? 0) - (a.health?.revenue_24h ?? 0))[0];
  const latest = rows
    .map((r) => r.health?.last_event_at)
    .filter((x): x is string => !!x)
    .sort()
    .pop();
  const capped = tenantTotal > MAX_TENANTS;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Overview</h1>
        {latest && <span className="freshness-badge">Latest event {relativeTime(latest)}</span>}
      </div>

      <DBQueryTime timings={timings} combined />

      {live.error && <ErrorMsg message={live.error} />}
      {rows.some((row) => row.refreshFailed) && <ErrorMsg message="Some store metrics could not refresh. Previous values are retained where available." />}
      <p className="hint">Live metrics ? refresh every 3 seconds</p>
      {alerts.length > 0 && (
        <section aria-label="Active alerts">
          {alerts.map((a) => (
            <div key={`${a.tenantName}-${a.code}`} className={`alert-strip alert-strip--${a.severity}`} role="alert">
              <strong>{a.tenantName}</strong>: {a.message}
            </div>
          ))}
        </section>
      )}

      <div className="stat-grid">
        <StatCard label="Gross revenue (7d)" numericValue={gross} currency={'INR'} value={formatCompactCurrency(gross)} fullValue={formatCurrency(gross)} accent="green" />
        <StatCard label="Net revenue (7d)" numericValue={net} currency={'INR'} value={formatCompactCurrency(net)} fullValue={formatCurrency(net)} accent="green" sub="after discounts" />
        <StatCard label="Avg order value (7d)" value={orders > 0 ? formatCompactCurrency(gross / orders) : '—'} fullValue={orders > 0 ? formatCurrency(gross / orders) : undefined} accent="blue" sub={`${formatCompactNumber(orders)} orders`} />
        <StatCard label="New customers (7d)" numericValue={newCustomers} value={formatCompactNumber(newCustomers)} fullValue={formatNumber(newCustomers)} accent="blue" />
        <StatCard label="Active tenants" numericValue={activeTenants} value={formatCompactNumber(activeTenants)} fullValue={formatNumber(activeTenants)} sub={`${formatCompactNumber(tenantTotal)} total`} accent="blue" />
        <StatCard
          label="Connected agents"
          numericValue={connectedAgents} value={formatCompactNumber(connectedAgents)}
          fullValue={formatNumber(connectedAgents)}
          sub={`${formatCompactNumber(agentList.length)} total`}
          accent={connectedAgents > 0 ? 'green' : 'amber'}
        />
      </div>

      <OperatorConfiguredMetrics tenantList={live.data.tenantList} />

      {top && top.health && (
        <section className="panel">
          <h2 className="panel-title">Top tenant · last 24 hours</h2>
          <p style={{ fontSize: 18, fontWeight: 600 }}>
            {top.tenant.name}{' '}
            <span className="hint" title={`${formatCurrency(top.health.revenue_24h)} from ${formatNumber(top.health.orders_24h)} orders`}>
              {formatCompactCurrency(top.health.revenue_24h)} from {formatCompactNumber(top.health.orders_24h)} orders
            </span>
          </p>
        </section>
      )}

      <section className="panel">
        <h2 className="panel-title">Tenant health</h2>
        <table className="table">
          <thead>
            <tr>
              <th>Tenant</th>
              <th>Plan</th>
              <th>Last event</th>
              <th>Last event delay</th>
              <th>Dead-letter</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(({ tenant, health }) => (
              <tr key={tenant.id}>
                <td>{tenant.name}<div className="mono">{tenant.external_tenant_id}</div></td>
                <td>{tenant.plan_tier}</td>
                <td>{health?.last_event_at ? relativeTime(health.last_event_at) : '—'}</td>
                <td className="mono">{health?.lag_seconds != null ? `${Math.round(health.lag_seconds)}s` : '—'}</td>
                <td>
                  {health ? (
                    <span className={health.dead_letter_count > 0 ? 'badge badge--red' : 'hint'}>{health.dead_letter_count}</span>
                  ) : '—'}
                </td>
                <td>
                  <Badge status={tenant.status} />
                  {health && <span className="hint"> ? {health.activity_state === 'idle' ? 'No recent activity' : health.activity_state}</span>}
                  {health && health.alerts.length > 0 && <span className="badge badge--amber" style={{ marginLeft: 6 }}>{health.alerts.length} alert{health.alerts.length > 1 ? 's' : ''}</span>}
                </td>
              </tr>
            ))}
            {rows.length === 0 && <tr><td colSpan={6} className="empty-cell">No tenants yet</td></tr>}
          </tbody>
        </table>
        {capped && <p className="hint">Showing figures for the first {MAX_TENANTS} active tenants of {tenantTotal}.</p>}
      </section>

      <section className="panel">
        <h2 className="panel-title">Agents</h2>
        <table className="table">
          <thead>
            <tr><th>Agent</th><th>Status</th><th>Last seen</th></tr>
          </thead>
          <tbody>
            {agentList.slice(0, 5).map((a) => (
              <tr key={a.id}>
                <td>{a.name}<div className="mono">{a.id}</div></td>
                <td><Badge status={a.status} /></td>
                <td>{a.last_seen_at ? relativeTime(a.last_seen_at) : '—'}</td>
              </tr>
            ))}
            {agentList.length === 0 && <tr><td colSpan={3} className="empty-cell">No agents registered</td></tr>}
          </tbody>
        </table>
      </section>
    </div>
  );
}

function OperatorConfiguredMetrics({ tenantList }: { tenantList: Tenant[] }) {
  const [selected, setSelected] = useState('');
  const tenant = tenantList.find((item) => item.id === selected)
    ?? tenantList.find((item) => item.status === 'active') ?? tenantList[0];
  if (!tenant) return null;
  return (
    <section>
      <label className="controls">Configured metrics tenant
        <select className="select-sm" value={tenant.id} onChange={(event) => setSelected(event.target.value)}>
          {tenantList.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select>
        <span className="hint">Last 7 UTC days</span>
      </label>
      <ConfiguredMetrics scope={{ ...utcDayRange(7), tenantId: tenant.id }} organizationId={tenant.organization_id} />
    </section>
  );
}
