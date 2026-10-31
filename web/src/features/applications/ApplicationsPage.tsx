import { Fragment, useCallback, useState } from 'react';
import {
  agents as agentsApi,
  applications as appsApi,
  auth,
  errMsg,
  organizations as orgsApi,
  tenants as tenantsApi,
} from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { Badge } from '../../components/Badge';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import { relativeTime } from '../../lib/utils';
import type { Agent, Application, Organization, Tenant } from '../../types';
import AgentDetail from './AgentDetail';

export default function ApplicationsPage() {
  const operator = auth.isOperator();
  const [apps, setApps] = useState<Application[]>([]);
  const [agentList, setAgentList] = useState<Agent[]>([]);
  const [tenantList, setTenantList] = useState<Tenant[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [appForm, setAppForm] = useState({ name: '', description: '', organization_id: '' });
  const [agentForm, setAgentForm] = useState({ name: '', application_id: '' });

  const load = useCallback(async () => {
    try {
      const [a, ag, t, o] = await Promise.all([
        appsApi.list(),
        agentsApi.list(),
        tenantsApi.list({ per_page: 200 }),
        orgsApi.list(),
      ]);
      setApps(a.data);
      setAgentList(ag.data);
      setTenantList(t.data.data ?? []);
      setOrgs(o.data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load applications'));
    } finally {
      setLoading(false);
    }
  }, []);

  useLoadEffect(load);

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      await load();
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const createApp = (e: React.FormEvent) => {
    e.preventDefault();
    void act(async () => {
      await appsApi.create({
        name: appForm.name,
        description: appForm.description || undefined,
        organization_id: operator ? appForm.organization_id : undefined,
      });
      setAppForm({ name: '', description: '', organization_id: '' });
    });
  };

  const createAgent = (e: React.FormEvent) => {
    e.preventDefault();
    void act(async () => {
      const res = await agentsApi.create(agentForm);
      setAgentForm({ name: '', application_id: '' });
      setExpanded(res.data.id); // open it so the first token can be issued right away
    });
  };

  const deleteApp = (a: Application) => {
    const n = agentList.filter((g) => g.application_id === a.id).length;
    if (!confirm(`Delete "${a.name}"${n ? ` and its ${n} agent(s), tokens and mappings` : ''}?`)) return;
    void act(() => appsApi.remove(a.id));
  };

  const renameApp = (a: Application) => {
    const name = prompt('New application name', a.name)?.trim();
    if (name && name !== a.name) void act(() => appsApi.update(a.id, { name }));
  };

  if (loading) return <Spinner />;

  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? id;
  const tenantsForAgent = (agent: Agent) => {
    const orgId = apps.find((a) => a.id === agent.application_id)?.organization_id;
    return tenantList.filter((t) => t.organization_id === orgId);
  };

  return (
    <div className="page">
      <div className="page-header">
        <h1>Applications & Integrations</h1>
      </div>

      {error && <ErrorMsg message={error} />}

      <section className="panel">
        <h2 className="panel-title">Connected Applications</h2>
        <form className="inline-form" onSubmit={createApp}>
          <label>Name
            <input required value={appForm.name} onChange={(e) => setAppForm({ ...appForm, name: e.target.value })} placeholder="Reference Store" />
          </label>
          <label>Description
            <input value={appForm.description} onChange={(e) => setAppForm({ ...appForm, description: e.target.value })} placeholder="optional" />
          </label>
          {operator && (
            <label>Organization
              <select required value={appForm.organization_id} onChange={(e) => setAppForm({ ...appForm, organization_id: e.target.value })}>
                <option value="">Select…</option>
                {orgs.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
              </select>
            </label>
          )}
          <button className="btn btn--primary btn--sm" type="submit">Register application</button>
        </form>

        {apps.length === 0 ? (
          <EmptyState message="No applications registered yet." />
        ) : (
          <table className="table">
            <thead>
              <tr><th>Name</th><th>Organization</th><th>Agents</th><th>Registered</th><th></th></tr>
            </thead>
            <tbody>
              {apps.map((app) => (
                <tr key={app.id}>
                  <td>{app.name}{app.description && <div className="hint">{app.description}</div>}</td>
                  <td className="mono">{app.organization_id}</td>
                  <td>{agentList.filter((g) => g.application_id === app.id).length}</td>
                  <td>{new Date(app.created_at).toLocaleDateString()}</td>
                  <td>
                    <div className="row-actions">
                      <button className="btn btn--sm" onClick={() => renameApp(app)}>Rename</button>
                      <button className="btn btn--sm btn--danger" onClick={() => deleteApp(app)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="panel">
        <h2 className="panel-title">Axyra Agents</h2>
        {apps.length > 0 && (
          <form className="inline-form" onSubmit={createAgent}>
            <label>Agent name
              <input required value={agentForm.name} onChange={(e) => setAgentForm({ ...agentForm, name: e.target.value })} placeholder="Orders DB agent" />
            </label>
            <label>Application
              <select required value={agentForm.application_id} onChange={(e) => setAgentForm({ ...agentForm, application_id: e.target.value })}>
                <option value="">Select…</option>
                {apps.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </label>
            <button className="btn btn--primary btn--sm" type="submit">Register agent</button>
          </form>
        )}

        {agentList.length === 0 ? (
          <EmptyState message="No agents registered." />
        ) : (
          <table className="table">
            <thead>
              <tr><th>Agent</th><th>Application</th><th>Status</th><th>Last seen</th><th></th></tr>
            </thead>
            <tbody>
              {agentList.map((a) => (
                <Fragment key={a.id}>
                  <tr>
                    <td>{a.name}<div className="mono">{a.id}</div></td>
                    <td>{appName(a.application_id)}</td>
                    <td><Badge status={a.status} /></td>
                    <td>{a.last_seen_at ? relativeTime(a.last_seen_at) : '—'}</td>
                    <td>
                      <button className="btn btn--sm" onClick={() => setExpanded(expanded === a.id ? null : a.id)}>
                        {expanded === a.id ? 'Close' : 'Manage'}
                      </button>
                    </td>
                  </tr>
                  {expanded === a.id && (
                    <tr>
                      <td colSpan={5}>
                        <AgentDetail
                          agent={a}
                          tenants={tenantsForAgent(a)}
                          onChanged={() => {
                            setExpanded(null);
                            void load();
                          }}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
