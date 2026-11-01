import { useState } from 'react';
import { analytics } from '../../../lib/api';
import { Badge } from '../../../components/Badge';
import { StatCard } from '../../../components/StatCard';
import { formatCompactCurrency, formatCompactNumber, formatCurrency, formatNumber, relativeTime } from '../../../lib/utils';
import { Bar, Empty, Panel } from './shared';
import { maskId, type PanelScope } from './format';
import { usePanel } from './usePanel';
import { TrendLineChart } from './TrendLineChart';

const key = (name: string, s: PanelScope, ...extra: Array<string | number>) =>
  [name, s.from, s.to, s.tenantId ?? '', ...extra].join('|');

/** Section B: customer counts and lifetime value. */
export function CustomerSummaryPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.customerSummary({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    key('cust-summary', scope)
  );
  const c = p.data;
  return (
    <Panel title="Customers" loading={p.loading} error={p.error} onRetry={p.reload}>
      {c && (
        <div className="stat-grid">
          <StatCard label="Customers" numericValue={c.total_customers} value={formatCompactNumber(c.total_customers)} fullValue={formatNumber(c.total_customers)} accent="blue" sub="who ordered in this period" />
          <StatCard label="New" numericValue={c.new_customers} value={formatCompactNumber(c.new_customers)} fullValue={formatNumber(c.new_customers)} accent="green" sub={`${c.new_customer_rate_pct}% of customers`} />
          <StatCard label="Returning" numericValue={c.returning_customers} value={formatCompactNumber(c.returning_customers)} fullValue={formatNumber(c.returning_customers)} />
          <StatCard label="Avg lifetime value" numericValue={c.avg_ltv} currency={'INR'} value={formatCompactCurrency(c.avg_ltv)} fullValue={formatCurrency(c.avg_ltv)} accent="green" />
          <StatCard label="Orders per customer" value={Math.abs(c.avg_orders_per_customer) >= 1000 ? formatCompactNumber(c.avg_orders_per_customer) : c.avg_orders_per_customer.toFixed(2)} fullValue={c.avg_orders_per_customer.toFixed(2)} />
        </div>
      )}
    </Panel>
  );
}

/** Section B: new versus returning orders over time, as two lines. */
export function CohortPanel({ scope }: { scope: PanelScope }) {
  const [interval, setInterval] = useState<'day' | 'week'>('day');
  const p = usePanel(
    () => analytics.cohorts({ from: scope.from, to: scope.to, interval, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('cohorts', scope, interval)
  );
  const pts = p.data ?? [];


  return (
    <Panel
      title="New vs returning customers"
      loading={p.loading}
      error={p.error}
      onRetry={p.reload}
      actions={
        <div className="controls">
          {(['day', 'week'] as const).map((i) => (
            <button key={i} className={`btn btn--sm ${interval === i ? 'btn--active' : ''}`} onClick={() => setInterval(i)}>
              {i}
            </button>
          ))}
        </div>
      }
    >
      {pts.length === 0 ? (
        <Empty />
      ) : (
        <TrendLineChart
          points={pts.map((pt) => ({ bucket: pt.bucket, value: pt.new_orders, secondary: pt.returning_orders }))}
          label="New customer orders"
          secondaryLabel="Returning customer orders"
          interval={interval}
          formatValue={formatNumber}
        />
      )}
    </Panel>
  );
}

/** Section B: highest-spending customers. Ids are shortened on screen. */
export function TopBuyersPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.topBuyers({ limit: 10, from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('top-buyers', scope)
  );
  const rows = p.data ?? [];
  return (
    <Panel title="Top buyers" loading={p.loading} error={p.error} onRetry={p.reload}>
      {rows.length === 0 ? (
        <Empty />
      ) : (
        <table className="table">
          <thead>
            <tr><th>#</th><th>Customer</th><th>Tier</th><th>Orders</th><th>Spend</th><th>Avg order</th><th>Last active</th></tr>
          </thead>
          <tbody>
            {rows.map((b, i) => (
              <tr key={b.customer_id}>
                <td className="mono">{i + 1}</td>
                <td className="mono" title="Shortened for display">{maskId(b.customer_id)}</td>
                <td><Badge status={b.customer_tier} /></td>
                <td>{formatNumber(b.order_count)}</td>
                <td>{formatCurrency(b.total_spend)}</td>
                <td>{formatCurrency(b.avg_order_value)}</td>
                <td>{b.last_order_at ? relativeTime(b.last_order_at) : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

/** Section C: revenue by country, state or city. */
export function GeoPanel({ scope }: { scope: PanelScope }) {
  const [by, setBy] = useState<'country' | 'state' | 'city'>('country');
  const p = usePanel(
    () => analytics.geo({ from: scope.from, to: scope.to, group_by: by, limit: 15, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('geo', scope, by)
  );
  const rows = p.data ?? [];
  const max = Math.max(...rows.map((r) => r.total_revenue), 1);
  return (
    <Panel
      title="Geography"
      loading={p.loading}
      error={p.error}
      onRetry={p.reload}
      actions={
        <div className="controls">
          {(['country', 'state', 'city'] as const).map((g) => (
            <button key={g} className={`btn btn--sm ${by === g ? 'btn--active' : ''}`} onClick={() => setBy(g)}>
              {g}
            </button>
          ))}
        </div>
      }
    >
      {rows.length === 0 ? (
        <Empty />
      ) : (
        <table className="table">
          <thead>
            <tr><th>Location</th><th>Orders</th><th>Revenue</th><th></th><th>Share</th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.country ?? ''}/${r.state_province ?? ''}/${r.group}`}>
                <td>
                  {r.group}
                  {by !== 'country' && <span className="hint"> {[r.state_province, r.country].filter(Boolean).join(', ')}</span>}
                </td>
                <td>{formatNumber(r.order_count)}</td>
                <td>{formatCurrency(r.total_revenue)}</td>
                <td style={{ width: 120 }}><Bar value={r.total_revenue} max={max} /></td>
                <td className="mono">{r.share_pct}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

/** Section D: which channel and referrer brought the orders. */
export function ChannelPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.channels({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('channels', scope)
  );
  const rows = p.data ?? [];
  const total = rows.reduce((s, r) => s + r.total_revenue, 0);
  return (
    <Panel title="Channels & acquisition" loading={p.loading} error={p.error} onRetry={p.reload}>
      {rows.length === 0 ? (
        <Empty />
      ) : (
        <table className="table">
          <thead>
            <tr><th>Channel</th><th>Source</th><th>Orders</th><th>Revenue</th><th>New customers</th><th>Share</th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.channel}/${r.referrer_source}`}>
                <td>{r.channel.replace('_', ' ')}</td>
                <td>{r.referrer_source || '—'}</td>
                <td>{formatNumber(r.order_count)}</td>
                <td>{formatCurrency(r.total_revenue)}</td>
                <td>{formatNumber(r.new_customers)}</td>
                <td className="mono">{total > 0 ? ((r.total_revenue / total) * 100).toFixed(1) : '0.0'}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
