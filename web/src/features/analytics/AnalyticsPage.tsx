import { useEffect, useState } from 'react';
import { analytics } from '../../lib/api';
import { Spinner, ErrorMsg } from '../../components/Feedback';
import { StatCard } from '../../components/StatCard';
import { formatCurrency, formatNumber, relativeTime, dateRangeParams } from '../../lib/utils';
import type { OrderSummary, TrendResponse, TopProductsResponse, BreakdownResponse, AnalyticsStatus } from '../../types';

const INTERVALS = ['hour', 'day'] as const;
const DAYS_OPTIONS = [1, 7, 30] as const;
const BREAKDOWN_OPTIONS = ['status', 'category'] as const;

export default function AnalyticsPage() {
  const [days, setDays] = useState<1 | 7 | 30>(7);
  const [interval, setInterval] = useState<'hour' | 'day'>('day');
  const [breakdownBy, setBreakdownBy] = useState<'status' | 'category'>('status');
  const [summary, setSummary] = useState<OrderSummary | null>(null);
  const [trend, setTrend] = useState<TrendResponse | null>(null);
  const [topProducts, setTopProducts] = useState<TopProductsResponse | null>(null);
  const [breakdown, setBreakdown] = useState<BreakdownResponse | null>(null);
  const [freshness, setFreshness] = useState<AnalyticsStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    const { from, to } = dateRangeParams(days);
    Promise.all([
      analytics.summary({ from, to }),
      analytics.trend({ interval, from, to }),
      analytics.topProducts({ limit: 10, from, to }),
      analytics.breakdown({ group_by: breakdownBy, from, to }),
      analytics.status(),
    ])
      .then(([s, tr, tp, bd, st]) => {
        setSummary(s.data);
        setTrend(tr.data);
        setTopProducts(tp.data);
        setBreakdown(bd.data);
        setFreshness(st.data);
      })
      .catch(() => setError('Failed to load analytics data.'))
      .finally(() => setLoading(false));
  }, [days, interval, breakdownBy]);

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  const cancellationRate =
    summary && summary.total_orders > 0
      ? ((summary.cancelled_orders / summary.total_orders) * 100).toFixed(1)
      : '0.0';

  return (
    <div className="page">
      <div className="page-header">
        <h1>Analytics</h1>
        <div className="controls">
          <span className="control-label">Range:</span>
          {DAYS_OPTIONS.map((d) => (
            <button
              key={d}
              className={`btn btn--sm ${days === d ? 'btn--active' : ''}`}
              onClick={() => setDays(d)}
            >
              {d === 1 ? '24h' : `${d}d`}
            </button>
          ))}
          <span className="control-label">Interval:</span>
          {INTERVALS.map((i) => (
            <button
              key={i}
              className={`btn btn--sm ${interval === i ? 'btn--active' : ''}`}
              onClick={() => setInterval(i)}
            >
              {i}
            </button>
          ))}
        </div>
      </div>

      {/* B-1: Freshness indicator */}
      {freshness && (
        <div className={`freshness-strip ${freshness.is_healthy ? 'freshness-strip--ok' : 'freshness-strip--warn'}`}>
          Data freshness: <strong>{relativeTime(freshness.last_processed_at)}</strong>
          &nbsp;·&nbsp; Lag: <strong>{freshness.lag_seconds.toFixed(1)}s</strong>
          &nbsp;·&nbsp; LSN: <code>{freshness.last_lsn}</code>
          {!freshness.is_healthy && <strong>&nbsp;·&nbsp; ⚠ Pipeline lag exceeds 5 minutes</strong>}
        </div>
      )}

      {/* FR-1: Summary counts and totals */}
      <div className="stat-grid">
        <StatCard label="Total Orders" value={formatNumber(summary?.total_orders ?? 0)} accent="blue" />
        <StatCard label="Total Revenue" value={formatCurrency(summary?.total_revenue ?? 0, summary?.currency)} accent="green" />
        <StatCard label="Active Orders" value={formatNumber(summary?.active_orders ?? 0)} />
        <StatCard label="Cancelled" value={`${formatNumber(summary?.cancelled_orders ?? 0)} (${cancellationRate}%)`} accent="red" />
      </div>

      {/* FR-2: Hourly/daily trend */}
      <section className="panel">
        <h2 className="panel-title">Orders Trend ({interval === 'hour' ? 'Hourly' : 'Daily'})</h2>
        {trend && trend.data.length > 0 ? (
          <div className="trend-chart">
            {trend.data.map((pt) => {
              const maxCount = Math.max(...trend.data.map((p) => p.order_count), 1);
              const pct = Math.max(Math.round((pt.order_count / maxCount) * 100), 2);
              const label =
                interval === 'day'
                  ? new Date(pt.bucket).toLocaleDateString('en-IN', { day: 'numeric', month: 'short' })
                  : new Date(pt.bucket).toLocaleTimeString('en-IN', { hour: '2-digit' });
              return (
                <div
                  key={pt.bucket}
                  className="trend-bar-wrap"
                  title={`${label}: ${formatNumber(pt.order_count)} orders · ${formatCurrency(pt.revenue)}`}
                >
                  <div className="trend-bar" style={{ height: `${pct}%` }} />
                  <span className="trend-label">{label}</span>
                </div>
              );
            })}
          </div>
        ) : (
          <p className="empty-state">No trend data for this period.</p>
        )}
      </section>

      <div className="panel-grid">
        {/* FR-3: Top-N products by revenue */}
        <section className="panel">
          <h2 className="panel-title">Top Products by Revenue</h2>
          <table className="table">
            <thead>
              <tr>
                <th>#</th>
                <th>Product</th>
                <th>Category</th>
                <th>Orders</th>
                <th>Revenue</th>
              </tr>
            </thead>
            <tbody>
              {topProducts?.data.map((p, i) => (
                <tr key={p.product_id}>
                  <td className="mono">{i + 1}</td>
                  <td>{p.product_name}</td>
                  <td>{p.category}</td>
                  <td>{formatNumber(p.order_count)}</td>
                  <td>{formatCurrency(p.revenue)}</td>
                </tr>
              ))}
              {!topProducts?.data.length && (
                <tr>
                  <td colSpan={5} className="empty-cell">No data for this period.</td>
                </tr>
              )}
            </tbody>
          </table>
        </section>

        {/* FR-4: Category/status breakdown */}
        <section className="panel">
          <div className="panel-head">
            <h2 className="panel-title">Order Breakdown</h2>
            <div className="controls">
              {BREAKDOWN_OPTIONS.map((opt) => (
                <button
                  key={opt}
                  className={`btn btn--sm ${breakdownBy === opt ? 'btn--active' : ''}`}
                  onClick={() => setBreakdownBy(opt)}
                >
                  By {opt}
                </button>
              ))}
            </div>
          </div>
          <table className="table">
            <thead>
              <tr>
                <th>{breakdownBy === 'status' ? 'Status' : 'Category'}</th>
                <th>Count</th>
                <th>Revenue</th>
                <th>Share</th>
              </tr>
            </thead>
            <tbody>
              {breakdown?.data.map((b) => {
                const totalCount = breakdown.data.reduce((s, x) => s + x.count, 0);
                const sharePct = totalCount > 0 ? ((b.count / totalCount) * 100).toFixed(1) : '0.0';
                return (
                  <tr key={b.group}>
                    <td>{b.group}</td>
                    <td>{formatNumber(b.count)}</td>
                    <td>{formatCurrency(b.revenue)}</td>
                    <td className="mono">{sharePct}%</td>
                  </tr>
                );
              })}
              {!breakdown?.data.length && (
                <tr>
                  <td colSpan={4} className="empty-cell">No breakdown data.</td>
                </tr>
              )}
            </tbody>
          </table>
        </section>
      </div>
    </div>
  );
}
