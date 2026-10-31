import { useState } from 'react';
import { analytics } from '../../../lib/api';
import { StatCard } from '../../../components/StatCard';
import { formatCompactCurrency, formatCompactNumber, formatCurrency, formatNumber } from '../../../lib/utils';
import { Empty, Panel } from './shared';
import type { PanelScope } from './format';
import { usePanel } from './usePanel';
import { TrendLineChart } from './TrendLineChart';

const key = (name: string, s: PanelScope, ...extra: Array<string | number>) =>
  [name, s.from, s.to, s.tenantId ?? '', ...extra].join('|');

/** Section A: gross, discounts, net, tax, shipping and order value. */
export function RevenueStrip({ scope }: { scope: PanelScope }) {
  const p = usePanel(
    () => analytics.revenue({ from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    key('revenue', scope)
  );
  const r = p.data;
  const cur = r?.currency;
  return (
    <Panel title="Revenue breakdown" loading={p.loading} error={p.error} onRetry={p.reload}>
      {r && (
        <div className="stat-grid">
          <StatCard label="Gross revenue" value={formatCompactCurrency(r.gross_revenue, cur)} fullValue={formatCurrency(r.gross_revenue, cur)} accent="green" />
          <StatCard label="Discounts" value={formatCompactCurrency(r.total_discount, cur)} fullValue={formatCurrency(r.total_discount, cur)} accent="amber" />
          <StatCard label="Net revenue" value={formatCompactCurrency(r.net_revenue, cur)} fullValue={formatCurrency(r.net_revenue, cur)} accent="green" sub="gross less discounts" />
          <StatCard label="Tax collected" value={formatCompactCurrency(r.total_tax, cur)} fullValue={formatCurrency(r.total_tax, cur)} />
          <StatCard label="Shipping" value={formatCompactCurrency(r.total_shipping, cur)} fullValue={formatCurrency(r.total_shipping, cur)} />
          <StatCard label="Avg order value" value={formatCompactCurrency(r.avg_order_value, cur)} fullValue={formatCurrency(r.avg_order_value, cur)} accent="blue" sub={`${formatCompactNumber(r.active_orders)} orders`} />
          <StatCard label="Units sold" value={formatCompactNumber(r.total_quantity)} fullValue={formatNumber(r.total_quantity)} />
        </div>
      )}
    </Panel>
  );
}

type Series = 'revenue' | 'net_revenue' | 'discount' | 'new_customers' | 'order_count';

const SERIES: Array<{ id: Series; label: string; money: boolean }> = [
  { id: 'order_count', label: 'Orders', money: false },
  { id: 'revenue', label: 'Gross revenue', money: true },
  { id: 'net_revenue', label: 'Net revenue', money: true },
  { id: 'discount', label: 'Discounts', money: true },
  { id: 'new_customers', label: 'New-customer orders', money: false },
];

/** Section A: the trend chart, with a choice of series. */
export function TrendPanel({ scope, interval }: { scope: PanelScope; interval: 'hour' | 'day' }) {
  const [series, setSeries] = useState<Series>('order_count');
  const p = usePanel(
    () => analytics.trend({ interval, from: scope.from, to: scope.to, tenant_id: scope.tenantId }).then((r) => r.data),
    key('trend', scope, interval)
  );
  const pts = p.data?.data ?? [];
  const meta = SERIES.find((s) => s.id === series)!;
  const fmt = (v: number) => (meta.money ? formatCurrency(v) : formatNumber(v));

  return (
    <Panel
      title={`Trend (${interval === 'hour' ? 'hourly' : 'daily'})`}
      loading={p.loading}
      error={p.error}
      onRetry={p.reload}
      actions={
        <div className="controls">
          {SERIES.map((s) => (
            <button key={s.id} className={`btn btn--sm ${series === s.id ? 'btn--active' : ''}`} onClick={() => setSeries(s.id)}>
              {s.label}
            </button>
          ))}
        </div>
      }
    >
      {pts.length === 0 ? (
        <Empty>No trend data for this period.</Empty>
      ) : (
        <TrendLineChart points={pts.map((pt) => ({ bucket: pt.bucket, value: pt[series] }))} label={meta.label} interval={interval} formatValue={fmt} />
      )}
    </Panel>
  );
}
