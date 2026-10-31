import { useEffect, useState } from 'react';
import { applications as appsApi, agents as agentsApi } from '../../lib/api';
import { Badge } from '../../components/Badge';
import { Spinner, ErrorMsg, EmptyState } from '../../components/Feedback';
import { relativeTime } from '../../lib/utils';
import type { Application, Agent } from '../../types';

export default function ApplicationsPage() {
  const [apps, setApps] = useState<Application[]>([]);
  const [agentList, setAgentList] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tokenMap, setTokenMap] = useState<Record<string, string>>({});
  const [revealedTokens, setRevealedTokens] = useState<Set<string>>(new Set());

  useEffect(() => {
    Promise.all([appsApi.list(), agentsApi.list()])
      .then(([a, ag]) => {
        setApps(a.data);
        setAgentList(ag.data);
      })
      .catch(() => setError('Failed to load applications'))
      .finally(() => setLoading(false));
  }, []);

  const issueToken = async (agentId: string) => {
    const res = await agentsApi.issueToken(agentId);
    setTokenMap((prev) => ({ ...prev, [agentId]: res.data.token }));
    setRevealedTokens((prev) => new Set(prev).add(agentId));
  };

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Applications & Integrations</h1>
      </div>

      <section className="panel">
        <h2 className="panel-title">Connected Applications</h2>
        {apps.length === 0 ? (
          <EmptyState message="No applications registered. Developer A registers these via the control-plane API." />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Organization</th>
                <th>Registered</th>
              </tr>
            </thead>
            <tbody>
              {apps.map((app) => (
                <tr key={app.id}>
                  <td>{app.name}</td>
                  <td className="mono">{app.organization_id}</td>
                  <td>{new Date(app.created_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="panel">
        <h2 className="panel-title">Axyra Agents</h2>
        {agentList.length === 0 ? (
          <EmptyState message="No agents registered." />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Agent ID</th>
                <th>Application</th>
                <th>Status</th>
                <th>Last Seen</th>
                <th>Credentials</th>
              </tr>
            </thead>
            <tbody>
              {agentList.map((a) => (
                <tr key={a.id}>
                  <td className="mono">{a.id}</td>
                  <td className="mono">{a.application_id}</td>
                  <td><Badge status={a.status} /></td>
                  <td>{a.last_seen_at ? relativeTime(a.last_seen_at) : '—'}</td>
                  <td>
                    {revealedTokens.has(a.id) ? (
                      <span className="mono token-revealed">{tokenMap[a.id]}</span>
                    ) : (
                      <button className="btn btn--sm" onClick={() => issueToken(a.id)}>
                        Issue Token
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

