import { useCallback, useState } from 'react';
import { Activity, AlertTriangle, CheckCircle, Clock, Database, Layers, XCircle, Zap } from 'lucide-react';
import { analytics, errMsg } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { ErrorMsg, Spinner } from '../../components/Feedback';
import { StatCard } from '../../components/StatCard';
import MonitoringDetailPanels from './MonitoringDetailPanels';
import { relativeTime } from '../../lib/utils';
import type { DeadLetterEvent, MonitoringStats } from '../../types';

export default function MonitoringPage() {
  const [stats, setStats] = useState<MonitoringStats | null>(null);
  const [deadLetters, setDeadLetters] = useState<DeadLetterEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [rebuildResult, setRebuildResult] = useState<string | null>(null);
  const [rebuildLoading, setRebuildLoading] = useState(false);
  // Bumped by Refresh so the independently-loaded detail panels reload with the rest.
  const [refreshKey, setRefreshKey] = useState(0);

  const load = useCallback(async () => {
    try {
      const [monResp, dlResp] = await Promise.all([
        analytics.monitoring(),
        analytics.deadLetter({ limit: 20 }),
      ]);
      setStats(monResp.data);
      setDeadLetters(dlResp.data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load monitoring data'));
    } finally {
      setLoading(false);
    }
  }, []);

  useLoadEffect(load);

  const handleRebuild = async () => {
    if (!confirm('Rebuild all aggregates for the current tenant from raw events? This may take a while.')) return;
    setRebuildLoading(true);
    setRebuildResult(null);
    try {
      const res = await analytics.rebuild();
      setRebuildResult(
        `Rebuild complete: ${res.data.events_replayed.toLocaleString()} events replayed in ${res.data.duration_ms}ms.`
      );
    } catch (err) {
      setRebuildResult(`Rebuild failed: ${errMsg(err)}`);
    } finally {
      setRebuildLoading(false);
    }
  };

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  const WorkerIcon = stats?.worker_healthy ? CheckCircle : XCircle;
  const workerColor = stats?.worker_healthy ? 'var(--green)' : 'var(--red)';

  return (
    <div className="page">
      <div className="page-header">
        <h1>Monitoring</h1>
        <div className="row-actions">
          <button className="btn btn--sm" onClick={() => { setLoading(true); setRefreshKey((k) => k + 1); void load(); }}>
            ↻ Refresh
          </button>
          <button
            className="btn btn--sm btn--danger"
            onClick={() => void handleRebuild()}
            disabled={rebuildLoading}
          >
            {rebuildLoading ? 'Rebuilding…' : '⟳ Rebuild aggregates'}
          </button>
        </div>
      </div>

      {/* B-2: Rebuild result banner */}
      {rebuildResult && (
        <div className={`freshness-strip ${rebuildResult.startsWith('Rebuild complete') ? 'freshness-strip--ok' : 'freshness-strip--warn'}`}>
          {rebuildResult}
        </div>
      )}

      {/* Worker health strip */}
      <div className={`freshness-strip ${stats?.worker_healthy ? 'freshness-strip--ok' : 'freshness-strip--warn'}`}>
        <WorkerIcon size={14} style={{ display: 'inline', verticalAlign: 'middle', color: workerColor, marginRight: 6 }} />
        Worker status: <strong>{stats?.worker_healthy ? 'Healthy' : 'Degraded'}</strong>
        {stats?.last_processed_at && (
          <>&nbsp;·&nbsp; Last processed: <strong>{relativeTime(stats.last_processed_at)}</strong></>
        )}
      </div>

      {/* B-5: Key operational stats */}
      <div className="stat-grid">
        <StatCard
          label="Queue Depth"
          value={stats?.queue_depth.toLocaleString() ?? '—'}
          accent={stats && stats.queue_depth > 500 ? 'amber' : undefined}
          sub="items waiting"
        />
        <StatCard
          label="Ingestion (60s)"
          value={stats?.events_last_60s.toLocaleString() ?? '—'}
          accent="blue"
          sub="events received"
        />
        <StatCard
          label="Ingestion (5m)"
          value={stats?.events_last_5m.toLocaleString() ?? '—'}
          sub="events received"
        />
        <StatCard
          label="Processed (5m)"
          value={stats?.processed_last_5m.toLocaleString() ?? '—'}
          accent="green"
          sub="events processed"
        />
        <StatCard
          label="Dead-letter"
          value={stats?.dead_letter_count.toLocaleString() ?? '—'}
          accent={stats && stats.dead_letter_count > 0 ? 'red' : undefined}
          sub="unresolved failures"
        />
      </div>

      {/* B-5: Query latency panel */}
      <section className="panel">
        <h2 className="panel-title">
          <Clock size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 6 }} />
          Query Latency (last 5 minutes)
        </h2>
        {stats && stats.latency_sample_count > 0 ? (
          <div className="monitoring-latency">
            <div className="monitoring-latency-row">
              <span className="monitoring-latency-label">p50</span>
              <LatencyBar ms={stats.latency_p50_ms} maxMs={stats.latency_p99_ms ?? stats.latency_p50_ms ?? 1} />
              <span className="monitoring-latency-value">{stats.latency_p50_ms != null ? `${stats.latency_p50_ms}ms` : '—'}</span>
            </div>
            <div className="monitoring-latency-row">
              <span className="monitoring-latency-label">p95</span>
              <LatencyBar ms={stats.latency_p95_ms} maxMs={stats.latency_p99_ms ?? stats.latency_p95_ms ?? 1} />
              <span className="monitoring-latency-value">{stats.latency_p95_ms != null ? `${stats.latency_p95_ms}ms` : '—'}</span>
            </div>
            <div className="monitoring-latency-row">
              <span className="monitoring-latency-label">p99</span>
              <LatencyBar ms={stats.latency_p99_ms} maxMs={stats.latency_p99_ms ?? 1} />
              <span className="monitoring-latency-value">{stats.latency_p99_ms != null ? `${stats.latency_p99_ms}ms` : '—'}</span>
            </div>
            <p className="hint" style={{ marginTop: 10 }}>
              Based on {stats.latency_sample_count.toLocaleString()} query samples.
            </p>
          </div>
        ) : (
          <p className="empty-state" style={{ padding: '20px 0' }}>
            No query latency data in the last 5 minutes.
          </p>
        )}
      </section>

      {/* B-5: Ingestion rate visualization */}
      <section className="panel">
        <h2 className="panel-title">
          <Zap size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 6 }} />
          Ingestion &amp; Processing Rate
        </h2>
        <div className="monitoring-rate-grid">
          <div className="monitoring-rate-item">
            <Activity size={20} style={{ color: 'var(--blue)' }} />
            <div>
              <div className="monitoring-rate-value">{stats?.events_last_60s.toLocaleString() ?? '—'}</div>
              <div className="monitoring-rate-label">events / last 60s</div>
            </div>
          </div>
          <div className="monitoring-rate-item">
            <Layers size={20} style={{ color: 'var(--accent)' }} />
            <div>
              <div className="monitoring-rate-value">{stats?.events_last_5m.toLocaleString() ?? '—'}</div>
              <div className="monitoring-rate-label">events / last 5m</div>
            </div>
          </div>
          <div className="monitoring-rate-item">
            <CheckCircle size={20} style={{ color: 'var(--green)' }} />
            <div>
              <div className="monitoring-rate-value">{stats?.processed_last_5m.toLocaleString() ?? '—'}</div>
              <div className="monitoring-rate-label">processed / last 5m</div>
            </div>
          </div>
          <div className="monitoring-rate-item">
            <Database size={20} style={{ color: stats && stats.queue_depth > 500 ? 'var(--amber)' : 'var(--text-muted)' }} />
            <div>
              <div className="monitoring-rate-value">{stats?.queue_depth.toLocaleString() ?? '—'}</div>
              <div className="monitoring-rate-label">queue depth</div>
            </div>
          </div>
        </div>
      </section>

      {/* Throughput, event types, schema versions, agent lag, failures by tenant */}
      <MonitoringDetailPanels refreshKey={refreshKey} />

      {/* B-5: Failed / dead-letter events */}
      <section className="panel">
        <div className="panel-head">
          <h2 className="panel-title">
            <AlertTriangle size={14} style={{ display: 'inline', verticalAlign: 'middle', marginRight: 6 }} />
            Dead-letter Events (last 20)
          </h2>
          {stats && stats.dead_letter_count > 0 && (
            <span className="badge badge--red">{stats.dead_letter_count} unresolved</span>
          )}
        </div>
        {deadLetters.length === 0 ? (
          <p className="empty-state" style={{ padding: '20px 0' }}>No failed events.</p>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>Agent</th>
                  <th>Event ID</th>
                  <th>LSN</th>
                  <th>Reason</th>
                  <th>Retries</th>
                  <th>Replayed</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {deadLetters.map((dl) => (
                  <tr key={dl.id}>
                    <td className="mono">{dl.id}</td>
                    <td className="mono">{dl.agent_id}</td>
                    <td className="mono" style={{ maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={dl.event_id}>
                      {dl.event_id || '—'}
                    </td>
                    <td className="mono">{dl.lsn || '—'}</td>
                    <td style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={dl.reason}>
                      {dl.reason}
                    </td>
                    <td className="mono">{dl.retry_count}</td>
                    <td>
                      {dl.replayed_at ? (
                        <span className="badge badge--green">replayed</span>
                      ) : (
                        <span className="badge badge--amber">pending</span>
                      )}
                    </td>
                    <td className="mono">{relativeTime(dl.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function LatencyBar({ ms, maxMs }: { ms?: number | null; maxMs?: number | null }) {
  const pct = ms != null && maxMs != null && maxMs > 0 ? Math.max(Math.round((ms / maxMs) * 100), 4) : 4;
  const color = ms == null ? 'var(--border)' : ms > 500 ? 'var(--red)' : ms > 100 ? 'var(--amber)' : 'var(--green)';
  return (
    <div className="monitoring-latency-bar-track">
      <div className="monitoring-latency-bar-fill" style={{ width: `${pct}%`, background: color }} />
    </div>
  );
}
