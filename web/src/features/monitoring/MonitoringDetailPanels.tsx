import { analytics } from '../../lib/api';
import { Badge } from '../../components/Badge';
import { formatNumber, relativeTime } from '../../lib/utils';
import { Empty, Panel } from '../analytics/panels/shared';
import { usePanel } from '../analytics/panels/usePanel';

/**
 * The extra monitoring panels: replication lag per agent, throughput, event
 * types, failures by tenant, and schema versions. They load independently of the
 * rest of the page, so a failure here cannot blank the original panels.
 */
export default function MonitoringDetailPanels({ refreshKey }: { refreshKey: number }) {
  const detail = usePanel(() => analytics.monitoringDetail().then((r) => r.data), `detail|${refreshKey}`);
  const rate = usePanel(() => analytics.ingestionRate().then((r) => r.data), `rate|${refreshKey}`);

  const d = detail.data;
  const points = rate.data?.data ?? [];
  const peak = Math.max(...points.map((p) => p.events), 1);
  const types = d?.event_types_5m ?? [];
  const typeMax = Math.max(...types.map((t) => t.count), 1);

  return (
    <>
      <Panel
        title={`Throughput · last 60 minutes${rate.data ? ` · ${formatNumber(rate.data.total_last_hour)} events` : ''}`}
        loading={rate.loading}
        error={rate.error}
        onRetry={rate.reload}
      >
        {points.length === 0 || rate.data?.total_last_hour === 0 ? (
          <Empty>No events processed in the last hour.</Empty>
        ) : (
          <>
            <div className="spark" role="img" aria-label="Events processed per minute">
              {points.map((p) => (
                <div
                  key={p.minute}
                  className="spark-bar"
                  style={{ height: `${Math.max((p.events / peak) * 100, p.events > 0 ? 4 : 0)}%` }}
                  title={`${new Date(p.minute).toLocaleTimeString('en-IN', { hour: '2-digit', minute: '2-digit' })}: ${p.events} events`}
                />
              ))}
            </div>
            <p className="hint">Peak {formatNumber(peak)} events/minute. Platform-wide.</p>
          </>
        )}
      </Panel>

      <div className="panel-grid">
        <Panel title="Event types · last 5 minutes" loading={detail.loading} error={detail.error} onRetry={detail.reload}>
          {types.length === 0 ? (
            <Empty>No events in the last 5 minutes.</Empty>
          ) : (
            <div className="funnel">
              {types.map((t) => (
                <div key={t.event_type} className="funnel-row">
                  <span className="funnel-label mono">{t.event_type}</span>
                  <div className="hbar-track">
                    <div className="hbar-fill" style={{ width: `${(t.count / typeMax) * 100}%`, background: 'var(--accent)' }} />
                  </div>
                  <span className="funnel-value">{formatNumber(t.count)}</span>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Schema versions · last 24 hours" loading={detail.loading} error={detail.error} onRetry={detail.reload}>
          {(d?.schema_versions_24h.length ?? 0) === 0 ? (
            <Empty>No events in the last 24 hours.</Empty>
          ) : (
            <>
              <table className="table">
                <thead><tr><th>Version</th><th>Events</th><th>First seen</th><th>Last seen</th></tr></thead>
                <tbody>
                  {d!.schema_versions_24h.map((v) => (
                    <tr key={v.schema_version}>
                      <td className="mono">v{v.schema_version}</td>
                      <td>{formatNumber(v.events)}</td>
                      <td>{relativeTime(v.first_seen)}</td>
                      <td>{relativeTime(v.last_seen)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {d!.schema_versions_24h.length > 1 && (
                <p className="hint">More than one version is arriving: some agents have not been upgraded yet.</p>
              )}
            </>
          )}
        </Panel>
      </div>

      <Panel title="Agent replication lag" loading={detail.loading} error={detail.error} onRetry={detail.reload}>
        {(d?.agents.length ?? 0) === 0 ? (
          <Empty>No agents registered.</Empty>
        ) : (
          <table className="table">
            <thead>
              <tr><th>Agent</th><th>Application</th><th>Confirmed LSN</th><th>Last seen</th><th>Lag</th><th>Status</th></tr>
            </thead>
            <tbody>
              {d!.agents.map((a) => (
                <tr key={a.agent_id}>
                  <td>{a.agent_name}<div className="mono">{a.agent_id}</div></td>
                  <td>{a.application}</td>
                  <td className="mono">{a.confirmed_lsn}</td>
                  <td>{a.last_seen_at ? relativeTime(a.last_seen_at) : '—'}</td>
                  <td className="mono">{a.lag_seconds != null ? `${Math.round(a.lag_seconds)}s` : '—'}</td>
                  <td><Badge status={a.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Failures by tenant" loading={detail.loading} error={detail.error} onRetry={detail.reload}>
        {(d?.dead_letter_by_tenant.length ?? 0) === 0 ? (
          <Empty>No unresolved failures.</Empty>
        ) : (
          <table className="table">
            <thead><tr><th>Tenant</th><th>Unresolved</th><th>Latest reason</th><th>Latest failure</th></tr></thead>
            <tbody>
              {d!.dead_letter_by_tenant.map((t) => (
                <tr key={t.tenant_id}>
                  <td className="mono">{t.tenant_id}</td>
                  <td><span className="badge badge--red">{formatNumber(t.unresolved)}</span></td>
                  <td style={{ maxWidth: 360, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={t.latest_reason}>
                    {t.latest_reason}
                  </td>
                  <td>{relativeTime(t.latest_failure_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}
