import { useState } from 'react';
import { analytics } from '../../../lib/api';
import { StatCard } from '../../../components/StatCard';
import { formatCompactCurrency, formatCompactNumber, formatCurrency, formatNumber } from '../../../lib/utils';
import { Bar, Empty, Panel } from './shared';
import type { PanelScope } from './format';
import { usePanel } from './usePanel';

const key = (name: string, s: PanelScope, ...extra: Array<string | number>) =>
  [name, s.from, s.to, s.tenantId ?? '', ...extra].join('|');

/** Section E: top products, sortable by revenue, units or returns. */
export function ProductsPanel({ scope }: { scope: PanelScope }) {
  const [sortBy, setSortBy] = useState<'revenue' | 'quantity' | 'returns'>('revenue');
  const p = usePanel(
    () =>
      analytics
        .topProducts({ limit: 10, from: scope.from, to: scope.to, sort_by: sortBy, tenant_id: scope.tenantId })
        .then((r) => r.data.data),
    key('products', scope, sortBy)
  );
  const rows = p.data ?? [];
  return (
    <Panel
      title="Top products"
      loading={p.loading}
      error={p.error}
      onRetry={p.reload}
      actions={
        <div className="controls">
          {(['revenue', 'quantity', 'returns'] as const).map((s) => (
            <button key={s} className={`btn btn--sm ${sortBy === s ? 'btn--active' : ''}`} onClick={() => setSortBy(s)}>
              by {s}
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
            <tr><th>#</th><th>Product</th><th>Category</th><th>Orders</th><th>Units</th><th>Revenue</th><th>Avg price</th><th>Returns</th></tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={r.product_id}>
                <td className="mono">{i + 1}</td>
                <td>{r.product_name}</td>
                <td>{r.category}</td>
                <td>{formatNumber(r.order_count)}</td>
                <td>{r.quantity_sold > 0 ? formatNumber(r.quantity_sold) : '—'}</td>
                <td>{formatCurrency(r.total_revenue)}</td>
                <td>{r.avg_unit_price > 0 ? formatCurrency(r.avg_unit_price) : '—'}</td>
                <td>
                  {formatNumber(r.return_count)} <span className="hint">({r.return_rate_pct}%)</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

/** Section F: how each payment method is performing. */
export function PaymentsPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.payments({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('payments', scope)
  );
  const rows = p.data ?? [];
  return (
    <Panel title="Payment health" loading={p.loading} error={p.error} onRetry={p.reload}>
      {rows.length === 0 ? (
        <Empty />
      ) : (
        <table className="table">
          <thead>
            <tr><th>Method</th><th>Orders</th><th>Paid</th><th>Failed</th><th>Refunded</th><th>Chargeback</th><th>Failure rate</th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.payment_method}>
                <td>{r.payment_method}</td>
                <td>{formatNumber(r.total_orders)}</td>
                <td>{formatNumber(r.paid_orders)}</td>
                <td>{formatNumber(r.failed_orders)}</td>
                <td>{formatNumber(r.refunded_orders)}</td>
                <td>{formatNumber(r.chargeback_orders)}</td>
                <td className="mono" style={{ color: r.failure_rate_pct > 5 ? 'var(--red)' : undefined, fontWeight: r.failure_rate_pct > 5 ? 700 : 400 }}>
                  {r.failure_rate_pct}%
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

const STAGE_COLOR: Record<string, string> = {
  pending: 'var(--text-muted)',
  processing: 'var(--blue)',
  shipped: 'var(--accent)',
  delivered: 'var(--green)',
  returned: 'var(--red)',
};

/** Section F: how many orders reached each fulfilment stage. */
export function FunnelPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.funnel({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data.data),
    key('funnel', scope)
  );
  const rows = p.data ?? [];
  const max = Math.max(...rows.map((r) => r.order_count), 1);
  return (
    <Panel title="Fulfilment funnel" loading={p.loading} error={p.error} onRetry={p.reload}>
      {rows.length === 0 || rows[0].order_count === 0 ? (
        <Empty />
      ) : (
        <div className="funnel">
          {rows.map((r, i) => {
            const prev = i > 0 && r.stage !== 'returned' ? rows[i - 1].order_count : null;
            const drop = prev !== null && prev > 0 ? ((prev - r.order_count) / prev) * 100 : null;
            return (
              <div key={r.stage} className="funnel-row">
                <span className="funnel-label">{r.stage}</span>
                <Bar value={r.order_count} max={max} color={STAGE_COLOR[r.stage]} />
                <span className="funnel-value">
                  {formatNumber(r.order_count)} <span className="hint">{r.pct_of_total}%</span>
                  {drop !== null && drop > 0 && <span className="hint"> · −{drop.toFixed(1)}%</span>}
                </span>
              </div>
            );
          })}
        </div>
      )}
      <p className="hint">Each stage counts orders that reached it or later. Cancelled orders are excluded.</p>
    </Panel>
  );
}

/** Section F: returns, refunds and why. */
export function ReturnsPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.returns({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    key('returns', scope)
  );
  const r = p.data;
  const max = Math.max(...(r?.top_reasons.map((x) => x.count) ?? [1]), 1);
  return (
    <Panel title="Returns & refunds" loading={p.loading} error={p.error} onRetry={p.reload}>
      {r && (
        <>
          <div className="stat-grid">
            <StatCard label="Returns" value={formatCompactNumber(r.total_returns)} fullValue={formatNumber(r.total_returns)} accent={r.total_returns > 0 ? 'amber' : undefined} />
            <StatCard label="Refunded" value={formatCompactCurrency(r.total_refund_amount)} fullValue={formatCurrency(r.total_refund_amount)} accent="red" />
            <StatCard label="Return rate" value={`${r.return_rate_pct}%`} accent={r.return_rate_pct > 15 ? 'red' : undefined} />
          </div>
          {r.top_reasons.length === 0 ? (
            <Empty>No returns in this period.</Empty>
          ) : (
            <div className="funnel">
              {r.top_reasons.map((x) => (
                <div key={x.reason} className="funnel-row">
                  <span className="funnel-label">{x.reason.replace('_', ' ')}</span>
                  <Bar value={x.count} max={max} color="var(--amber)" />
                  <span className="funnel-value">
                    {formatNumber(x.count)} <span className="hint">{x.share_pct}%</span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </Panel>
  );
}

/** Section G: promo usage and cost. */
export function DiscountsPanel({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.discounts({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    key('discounts', scope)
  );
  const d = p.data;
  return (
    <Panel title="Discounts & promos" loading={p.loading} error={p.error} onRetry={p.reload}>
      {d && (
        <>
          <div className="stat-grid">
            <StatCard label="Orders with a code" value={formatCompactNumber(d.orders_with_discount)} fullValue={formatNumber(d.orders_with_discount)} accent="blue" sub={`${d.discount_rate_pct}% of orders`} />
            <StatCard label="Discount given" value={formatCompactCurrency(d.total_discount_given)} fullValue={formatCurrency(d.total_discount_given)} accent="amber" />
            <StatCard label="Avg per discounted order" value={formatCompactCurrency(d.avg_discount_per_order)} fullValue={formatCurrency(d.avg_discount_per_order)} />
          </div>
          {d.top_codes.length === 0 ? (
            <Empty>No promo codes were used in this period.</Empty>
          ) : (
            <table className="table">
              <thead><tr><th>Code</th><th>Uses</th><th>Total discount</th></tr></thead>
              <tbody>
                {d.top_codes.map((c) => (
                  <tr key={c.code}>
                    <td className="mono">{c.code}</td>
                    <td>{formatNumber(c.uses)}</td>
                    <td>{formatCurrency(c.total_discount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </Panel>
  );
}
