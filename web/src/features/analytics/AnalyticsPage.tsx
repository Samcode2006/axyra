import { useEffect, useState } from 'react';
import { analytics } from '../../lib/api';
import { Spinner, ErrorMsg } from '../../components/Feedback';
import { StatCard } from '../../components/StatCard';
import { formatCurrency, formatNumber, relativeTime, dateRangeParams } from '../../lib/utils';
import type { OrderSummary, TrendResponse, TopProductsResponse, BreakdownResponse, AnalyticsStatus } from '../../types';

const INTERVALS = ['hour', 'day'] as const;
const DAYS_OPTIONS = [1, 7, 30] as const;

export default function AnalyticsPage() {
  const [days, setDays] = useState<1 | 7 | 30>(7);
  const [interval, setInterval] = useState<'hour' | 'day'>('day');
  const [summary, setSummary] = useState<OrderSummary | null>(null);
  const [trend, setTrend] = useState<TrendResponse | null>(null);
  const [topProducts, setTopProducts] = useState<TopProductsResponse | null>(null);
  const [statusBreakdown, setStatusBreakdown] = useState<BreakdownResponse | null>(null);
  const [freshness, setFreshness] = useState<AnalyticsStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    const { from, to } = dateRangeParams(days);
    Promise.all([
      analytics.summary({ from, to }),
      analytics.trend({ interval, from, to }),
      analytics.topProducts({ limit: 10, from, to }),
      analytics.breakdown({ group_by: 'status', from, to }),
      analytics.status(),
    ])
      .then(([s, tr, tp, bd, st]) => {
        setSummary(s.data);
        setTrend(tr.data);
        setTopProducts(tp.data);
        setStatusBreakdown(bd.data);
        setFreshness(st.data);
      })
      .catch(() => setError('Failed to load analytics data.'))
      .finally(() => setLoading(false));
  }, [days, interval]);

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

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
        <div className={`freshness-strip ${freshness.lag_seconds < 30 ? 'freshness-strip--ok' : 'freshness-strip--warn'}`}>
          Data freshness: <strong>{relativeTime(freshness.last_processed_at)}</strong>
          &nbsp;·&nbsp; Lag: <strong>{freshness.lag_seconds}s</strong>
          &nbsp;·&nbsp; LSN: <code>{freshness.last_lsn}</code>
        </div>
      )}

      {/* FR-1: Summary */}
      <div className="stat-grid">
        <StatCard label="Total Orders" value={formatNumber(summary?.total_orders ?? 0)} accent="blue" />
        <StatCard label="Total Revenue" value={formatCurrency(summary?.total_revenue ?? 0, summary?.currency)} accent="green" />
        <StatCard label="Active Orders" value={formatNumber(summary?.active_orders ?? 0)} />
        <StatCard label="Cancelled Orders" value={formatNumber(summary?.cancelled_orders ?? 0)} accent="red" />
      </div>

      {/* FR-2: Trend */}
      <section className="panel">
        <h2 className="panel-title">Orders Trend</h2>
        {trend && trend.data.length > 0 ? (
          <div className="trend-chart">
            {trend.data.map((pt) => {
              const maxCount = Math.max(...trend.data.map((p) => p.order_count), 1);
              const pct = Math.round((pt.order_count / maxCount) * 100);
              return (
                <div key={pt.bucket} className="trend-bar-wrap" title={`${pt.bucket}: ${pt.order_count} orders`}>
                  <div className="trend-bar" style={{ height: `${pct}%` }} />
                  <span className="trend-label">
                    {interval === 'day'
                      ? new Date(pt.bucket).toLocaleDateString('en-IN', { day: 'numeric', month: 'short' })
                      : new Date(pt.bucket).toLocaleTimeString('en-IN', { hour: '2-digit' })}
                  </span>
                </div>
              );
            })}
          </div>
        ) : (
          <p className="empty-state">No trend data yet.</p>
        )}
      </section>

      <div className="panel-grid">
        {/* FR-3: Top-N */}
        <section className="panel">
          <h2 className="panel-title">Top Products</h2>
          <table className="table">
            <thead>
              <tr><th>#</th><th>Product</th><th>Category</th><th>Orders</th><th>Revenue</th></tr>
            </thead>
            <tbody>
              {topProducts?.data.map((p, i) => (
                <tr key={p.product_id}>
                  <td>{i + 1}</td>
                  <td>{p.product_name}</td>
                  <td>{p.category}</td>
                  <td>{formatNumber(p.order_count)}</td>
                  <td>{formatCurrency(p.revenue)}</td>
                </tr>
              ))}
              {!topProducts?.data.length && (
                <tr><td colSpan={5} className="empty-cell">No data</td></tr>
              )}
            </tbody>
          </table>
        </section>

        {/* FR-4: Breakdown */}
        <section className="panel">
          <h2 className="panel-title">Order Status Breakdown</h2>
          <table className="table">
            <thead>
              <tr><th>Status</th><th>Count</th><th>Revenue</th></tr>
            </thead>
            <tbody>
              {statusBreakdown?.data.map((b) => (
                <tr key={b.group}>
                  <td>{b.group}</td>
                  <td>{formatNumber(b.count)}</td>
                  <td>{formatCurrency(b.revenue)}</td>
                </tr>
              ))}
              {!statusBreakdown?.data.length && (
                <tr><td colSpan={3} className="empty-cell">No data</td></tr>
              )}
            </tbody>
          </table>
        </section>
      </div>
    </div>
  );
}

