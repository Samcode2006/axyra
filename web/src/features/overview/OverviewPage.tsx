import { useEffect, useState } from 'react';
import { StatCard } from '../../components/StatCard';
import { Badge } from '../../components/Badge';
import { Spinner, ErrorMsg } from '../../components/Feedback';
import { analytics, tenants, agents, errMsg } from '../../lib/api';
import { formatCurrency, formatNumber, relativeTime } from '../../lib/utils';
import type { OrderSummary, AnalyticsStatus, Tenant, Agent } from '../../types';

export default function OverviewPage() {
  const [summary, setSummary] = useState<OrderSummary | null>(null);
  const [status, setStatus] = useState<AnalyticsStatus | null>(null);
  const [tenantList, setTenantList] = useState<Tenant[]>([]);
  const [agentList, setAgentList] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const to = new Date().toISOString();
    const from = new Date(Date.now() - 7 * 86_400_000).toISOString();
    // Management data and analytics are independent: an organization with no
    // tenants yet (or an analytics outage) must not hide the management data.
    Promise.all([tenants.list(), agents.list()])
      .then(([t, a]) => {
        setTenantList(t.data.data ?? []);
        setAgentList(a.data);
      })
      .catch((err) => setError(errMsg(err, 'Could not load overview data. Are the services running?')))
      .finally(() => setLoading(false));

    analytics.summary({ from, to }).then((s) => setSummary(s.data)).catch(() => setSummary(null));
    analytics.status().then((s) => setStatus(s.data)).catch(() => setStatus(null));
  }, []);

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  const activeTenants = tenantList.filter((t) => t.status === 'active').length;
  const connectedAgents = agentList.filter((a) => a.status === 'connected').length;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Overview</h1>
        {status && (
          <span className="freshness-badge">
            🟢 Data as of {relativeTime(status.last_processed_at)}
          </span>
        )}
      </div>

      <div className="stat-grid">
        <StatCard
          label="Total Orders (7d)"
          value={summary ? formatNumber(summary.total_orders) : '—'}
          accent="blue"
        />
        <StatCard
          label="Total Revenue (7d)"
          value={summary ? formatCurrency(summary.total_revenue, summary.currency) : '—'}
          accent="green"
        />
        <StatCard
          label="Active Tenants"
          value={activeTenants}
          sub={`${tenantList.length} total`}
          accent="blue"
        />
        <StatCard
          label="Connected Agents"
          value={connectedAgents}
          sub={`${agentList.length} total`}
          accent={connectedAgents > 0 ? 'green' : 'amber'}
        />
        {status && (
          <StatCard
            label="Processing Lag"
            value={`${status.lag_seconds}s`}
            sub={`LSN: ${status.last_lsn}`}
            accent={status.lag_seconds < 30 ? 'green' : 'amber'}
          />
        )}
      </div>

      <div className="panel-grid">
        <section className="panel">
          <h2 className="panel-title">Tenants</h2>
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>External ID</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {tenantList.slice(0, 5).map((t) => (
                <tr key={t.id}>
                  <td>{t.name}</td>
                  <td className="mono">{t.external_tenant_id}</td>
                  <td><Badge status={t.status} /></td>
                </tr>
              ))}
              {tenantList.length === 0 && (
                <tr><td colSpan={3} className="empty-cell">No tenants yet</td></tr>
              )}
            </tbody>
          </table>
        </section>

        <section className="panel">
          <h2 className="panel-title">Agents</h2>
          <table className="table">
            <thead>
              <tr>
                <th>Agent ID</th>
                <th>Status</th>
                <th>Last seen</th>
              </tr>
            </thead>
            <tbody>
              {agentList.slice(0, 5).map((a) => (
                <tr key={a.id}>
                  <td className="mono">{a.id}</td>
                  <td><Badge status={a.status} /></td>
                  <td>{a.last_seen_at ? relativeTime(a.last_seen_at) : '—'}</td>
                </tr>
              ))}
              {agentList.length === 0 && (
                <tr><td colSpan={3} className="empty-cell">No agents registered</td></tr>
              )}
            </tbody>
          </table>
        </section>
      </div>
    </div>
  );
}

