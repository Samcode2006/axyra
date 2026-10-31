import { useEffect, useMemo, useState } from 'react';
import { analytics, tenants as tenantsApi } from '../../lib/api';
import { StatCard } from '../../components/StatCard';
import { formatCurrency, formatNumber, relativeTime, utcDayRange } from '../../lib/utils';
import type { AnalyticsStatus, BreakdownGroup, Tenant } from '../../types';
import { CohortPanel, ChannelPanel, CustomerSummaryPanel, GeoPanel, TopBuyersPanel } from './panels/CustomerPanels';
import { RevenueStrip, TrendPanel } from './panels/FinancialPanels';
import { DiscountsPanel, FunnelPanel, PaymentsPanel, ProductsPanel, ReturnsPanel } from './panels/OperationsPanels';
import type { PanelScope } from './panels/format';
import { Empty, Panel } from './panels/shared';
import { usePanel } from './panels/usePanel';

const RANGES = [
  { days: 1, label: 'Today' },
  { days: 7, label: '7d' },
  { days: 30, label: '30d' },
] as const;

const BREAKDOWNS: Array<{ id: BreakdownGroup; label: string }> = [
  { id: 'status', label: 'Status' },
  { id: 'category', label: 'Category' },
  { id: 'payment_method', label: 'Payment' },
  { id: 'channel', label: 'Channel' },
  { id: 'country', label: 'Country' },
  { id: 'customer_tier', label: 'Tier' },
  { id: 'fulfillment_status', label: 'Fulfilment' },
  { id: 'referrer_source', label: 'Referrer' },
  { id: 'return_reason', label: 'Return reason' },
];

export default function AnalyticsPage() {
  const [days, setDays] = useState<1 | 7 | 30>(7);
  const [interval, setInterval] = useState<'hour' | 'day'>('day');
  const [tenantId, setTenantId] = useState<string>('');
  const [tenantList, setTenantList] = useState<Tenant[]>([]);

  // Whole UTC days, so the hourly and daily panels agree on where a range starts.
  const range = useMemo(() => utcDayRange(days), [days]);

  useEffect(() => {
    tenantsApi
      .list({ per_page: 200 })
      .then((r) => {
        const list = r.data.data ?? [];
        setTenantList(list);
        setTenantId((cur) => cur || list.find((t) => t.status === 'active')?.id || list[0]?.id || '');
      })
      .catch(() => setTenantList([]));
  }, []);

  const scope: PanelScope = { from: range.from, to: range.to, tenantId: tenantId || undefined };
  const tenantName = tenantList.find((t) => t.id === tenantId)?.name;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Analytics{tenantName ? <span className="hint"> · {tenantName}</span> : null}</h1>
        <div className="controls">
          {tenantList.length > 1 && (
            <>
              <span className="control-label">Tenant:</span>
              <select className="select-sm" value={tenantId} onChange={(e) => setTenantId(e.target.value)}>
                {tenantList.map((t) => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </select>
            </>
          )}
          <span className="control-label">Range:</span>
          {RANGES.map((r) => (
            <button key={r.days} className={`btn btn--sm ${days === r.days ? 'btn--active' : ''}`} onClick={() => setDays(r.days)}>
              {r.label}
            </button>
          ))}
          <span className="control-label">Interval:</span>
          {(['hour', 'day'] as const).map((i) => (
            <button key={i} className={`btn btn--sm ${interval === i ? 'btn--active' : ''}`} onClick={() => setInterval(i)}>
              {i}
            </button>
          ))}
        </div>
      </div>

      <p className="hint">Ranges cover whole UTC days. Customer, geography, payment and return figures are by the day an order was created.</p>

      {/* Waiting for the tenant list avoids a first request with no tenant and a second with one. */}
      {tenantList.length === 0 ? (
        <Empty>No tenants yet. Register one under Tenants to see analytics.</Empty>
      ) : (
        <>
          <FreshnessAndSummary scope={scope} />

          <h2 className="section-title">Revenue &amp; financials</h2>
          <RevenueStrip scope={scope} />
          <TrendPanel scope={scope} interval={interval} />

          <h2 className="section-title">Customers</h2>
          <CustomerSummaryPanel scope={scope} />
          <div className="panel-grid">
            <CohortPanel scope={scope} />
            <TopBuyersPanel scope={scope} />
          </div>

          <h2 className="section-title">Geography &amp; acquisition</h2>
          <div className="panel-grid">
            <GeoPanel scope={scope} />
            <ChannelPanel scope={scope} />
          </div>

          <h2 className="section-title">Products</h2>
          <ProductsPanel scope={scope} />

          <h2 className="section-title">Operations</h2>
          <div className="panel-grid">
            <PaymentsPanel scope={scope} />
            <FunnelPanel scope={scope} />
          </div>
          <ReturnsPanel scope={scope} />

          <h2 className="section-title">Discounts &amp; promos</h2>
          <DiscountsPanel scope={scope} />

          <h2 className="section-title">Breakdown</h2>
          <BreakdownPanel scope={scope} />
        </>
      )}
    </div>
  );
}

/** The data-freshness strip and the four headline cards (FR-1, B-1). */
function FreshnessAndSummary({ scope }: { scope: PanelScope }) {
  const k = [scope.from, scope.to, scope.tenantId ?? ''].join('|');
  const status = usePanel<AnalyticsStatus>(
    () => analytics.status({ tenant_id: scope.tenantId }).then((r) => r.data),
    'status|' + k
  );
  const summary = usePanel(
    () => analytics.summary({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    'summary|' + k
  );
  const f = status.data;
  const s = summary.data;
  const cancelRate = s && s.total_orders > 0 ? ((s.cancelled_orders / s.total_orders) * 100).toFixed(1) : '0.0';

  return (
    <>
      {f && (
        <div className={`freshness-strip ${f.is_healthy ? 'freshness-strip--ok' : 'freshness-strip--warn'}`}>
          Last event: <strong>{relativeTime(f.last_processed_at)}</strong>
          &nbsp;·&nbsp; Age: <strong>{f.lag_seconds.toFixed(0)}s</strong>
          &nbsp;·&nbsp; LSN: <code>{f.last_lsn}</code>
          {!f.is_healthy && <strong>&nbsp;·&nbsp; ⚠ No events for over 5 minutes</strong>}
        </div>
      )}
      <Panel title="Summary" loading={summary.loading} error={summary.error} onRetry={summary.reload}>
        {s && (
          <div className="stat-grid">
            <StatCard label="Total orders" value={formatNumber(s.total_orders)} accent="blue" />
            <StatCard label="Revenue" value={formatCurrency(s.total_revenue, s.currency)} accent="green" />
            <StatCard label="Active orders" value={formatNumber(s.active_orders)} />
            <StatCard label="Cancelled" value={`${formatNumber(s.cancelled_orders)} (${cancelRate}%)`} accent="red" />
          </div>
        )}
      </Panel>
    </>
  );
}

/** FR-4: orders grouped by any of the supported dimensions. */
function BreakdownPanel({ scope }: { scope: PanelScope }) {
  const [by, setBy] = useState<BreakdownGroup>('status');
  const p = usePanel(
    () => analytics.breakdown({ group_by: by, from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data.data),
    ['breakdown', scope.from, scope.to, scope.tenantId ?? '', by].join('|')
  );
  const rows = p.data ?? [];
  const total = rows.reduce((sum, x) => sum + x.count, 0);
  return (
    <Panel
      title="Order breakdown"
      loading={p.loading}
      error={p.error}
      onRetry={p.reload}
      actions={
        <select className="select-sm" value={by} onChange={(e) => setBy(e.target.value as BreakdownGroup)} aria-label="Group by">
          {BREAKDOWNS.map((b) => (
            <option key={b.id} value={b.id}>{b.label}</option>
          ))}
        </select>
      }
    >
      {rows.length === 0 ? (
        <Empty />
      ) : (
        <table className="table">
          <thead>
            <tr><th>{BREAKDOWNS.find((b) => b.id === by)?.label}</th><th>Orders</th><th>Revenue</th><th>Share</th></tr>
          </thead>
          <tbody>
            {rows.map((b) => (
              <tr key={b.group}>
                <td>{b.group || '—'}</td>
                <td>{formatNumber(b.count)}</td>
                <td>{formatCurrency(b.revenue)}</td>
                <td className="mono">{total > 0 ? ((b.count / total) * 100).toFixed(1) : '0.0'}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {by !== 'status' && by !== 'category' && (
        <p className="hint">Counts every order, cancelled included. Orders from before schema v2 are not shown for this grouping.</p>
      )}
    </Panel>
  );
}
