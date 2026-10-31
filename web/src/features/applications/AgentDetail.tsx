import { useCallback, useState } from 'react';
import { agents as agentsApi, errMsg } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { Badge } from '../../components/Badge';
import { ErrorMsg, Spinner } from '../../components/Feedback';
import { SecretBanner } from '../../components/SecretBanner';
import { formatDate } from '../../lib/utils';
import type { Agent, AgentToken, IssuedToken, Tenant, TenantMapping } from '../../types';

interface Props {
  agent: Agent;
  /** Tenants of the agent's organization, used to pick mapping targets. */
  tenants: Tenant[];
  onChanged: () => void;
}

const GRACE_OPTIONS = [
  { label: 'Revoke old token immediately', seconds: 0 },
  { label: 'Keep old token for 1 hour', seconds: 3600 },
  { label: 'Keep old token for 24 hours', seconds: 86_400 },
];

export default function AgentDetail({ agent, tenants, onChanged }: Props) {
  const [tokens, setTokens] = useState<AgentToken[]>([]);
  const [mappings, setMappings] = useState<TenantMapping[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [issued, setIssued] = useState<IssuedToken | null>(null);
  const [tokenForm, setTokenForm] = useState({ name: '', days: '' });
  const [grace, setGrace] = useState(0);
  const [mapForm, setMapForm] = useState({ external_tenant_id: '', tenant_id: '' });

  const load = useCallback(async () => {
    try {
      const [t, m] = await Promise.all([agentsApi.tokens(agent.id), agentsApi.mappings(agent.id)]);
      setTokens(t.data);
      setMappings(m.data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load agent details'));
    } finally {
      setLoading(false);
    }
  }, [agent.id]);

  useLoadEffect(load);

  const run = async (fn: () => Promise<void>) => {
    setError(null);
    try {
      await fn();
      await load();
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const issue = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      const days = tokenForm.days ? Number(tokenForm.days) : undefined;
      const res = await agentsApi.issueToken(agent.id, {
        name: tokenForm.name || undefined,
        expires_in_days: days,
      });
      setIssued(res.data);
      setTokenForm({ name: '', days: '' });
    });
  };

  const rotate = (t: AgentToken) => {
    if (!confirm(`Rotate ${t.token_prefix}…? You must deploy the new token to the agent.`)) return;
    void run(async () => {
      const res = await agentsApi.rotateToken(agent.id, t.id, { grace_period_seconds: grace });
      setIssued(res.data);
    });
  };

  const revoke = (t: AgentToken) => {
    if (!confirm(`Revoke ${t.token_prefix}…? An agent using it will stop being accepted.`)) return;
    void run(async () => {
      await agentsApi.revokeToken(agent.id, t.id);
    });
  };

  const addMapping = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await agentsApi.createMapping(agent.id, mapForm);
      setMapForm({ external_tenant_id: '', tenant_id: '' });
    });
  };

  const removeMapping = (m: TenantMapping) => {
    if (!confirm(`Remove mapping ${m.external_tenant_id}? Events for it will no longer resolve.`)) return;
    void run(async () => {
      await agentsApi.removeMapping(agent.id, m.id);
    });
  };

  const rename = () => {
    const name = prompt('New agent name', agent.name)?.trim();
    if (!name || name === agent.name) return;
    void run(async () => {
      await agentsApi.update(agent.id, { name });
      onChanged();
    });
  };

  const remove = () => {
    if (!confirm(`Delete agent "${agent.name}"? Its tokens and tenant mappings are deleted too.`)) return;
    void run(async () => {
      await agentsApi.remove(agent.id);
      onChanged();
    });
  };

  if (loading) return <Spinner />;

  const tenantName = (id: string) => tenants.find((t) => t.id === id)?.name ?? id;

  return (
    <div className="stack">
      {error && <ErrorMsg message={error} />}

      {issued && (
        <SecretBanner
          title={issued.previous_token_id ? 'New agent token (rotated)' : 'New agent token'}
          secret={issued.token}
          hint={
            issued.previous_token_expires_at
              ? `Set it as AXYRA_AGENT_TOKEN on the agent. The previous token keeps working until ${formatDate(issued.previous_token_expires_at)}.`
              : 'Set it as AXYRA_AGENT_TOKEN on the agent. It is not stored and cannot be shown again.'
          }
          onDismiss={() => setIssued(null)}
        />
      )}

      <div className="row-actions">
        <button className="btn btn--sm" onClick={rename}>Rename</button>
        <button className="btn btn--sm btn--danger" onClick={remove}>Delete agent</button>
      </div>

      <div className="subpanel">
        <h3>Tokens</h3>
        <form className="inline-form" onSubmit={issue}>
          <label>Name (optional)
            <input value={tokenForm.name} onChange={(e) => setTokenForm({ ...tokenForm, name: e.target.value })} placeholder="production" />
          </label>
          <label>Expires in days (optional)
            <input type="number" min={1} max={3650} value={tokenForm.days} onChange={(e) => setTokenForm({ ...tokenForm, days: e.target.value })} placeholder="never" />
          </label>
          <button className="btn btn--primary btn--sm" type="submit">Issue token</button>
        </form>

        <label className="inline-form">
          <span className="hint">When rotating:</span>
          <select value={grace} onChange={(e) => setGrace(Number(e.target.value))}>
            {GRACE_OPTIONS.map((o) => <option key={o.seconds} value={o.seconds}>{o.label}</option>)}
          </select>
        </label>

        {tokens.length === 0 ? (
          <p className="hint">No tokens yet. Issue one to connect the agent.</p>
        ) : (
          <table className="table">
            <thead>
              <tr><th>Token</th><th>Name</th><th>Status</th><th>Created</th><th>Expires</th><th></th></tr>
            </thead>
            <tbody>
              {tokens.map((t) => (
                <tr key={t.id}>
                  <td className="mono">{t.token_prefix}…</td>
                  <td>{t.name ?? '—'}</td>
                  <td><Badge status={t.status === 'active' ? 'active' : t.status === 'expired' ? 'suspended' : 'error'} /> <span className="hint">{t.status}</span></td>
                  <td>{formatDate(t.created_at)}</td>
                  <td>{t.expires_at ? formatDate(t.expires_at) : 'never'}</td>
                  <td>
                    {t.status === 'active' && (
                      <div className="row-actions">
                        <button className="btn btn--sm" onClick={() => rotate(t)}>Rotate</button>
                        <button className="btn btn--sm btn--danger" onClick={() => revoke(t)}>Revoke</button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="subpanel">
        <h3>Tenant mappings</h3>
        <p className="hint">
          Maps the tenant id found in the source database row (for example the <code>store_id</code> column) to an Axyra tenant.
        </p>
        <form className="inline-form" onSubmit={addMapping}>
          <label>External tenant id
            <input required value={mapForm.external_tenant_id} onChange={(e) => setMapForm({ ...mapForm, external_tenant_id: e.target.value })} placeholder="store_001" />
          </label>
          <label>Axyra tenant
            <select required value={mapForm.tenant_id} onChange={(e) => setMapForm({ ...mapForm, tenant_id: e.target.value })}>
              <option value="">Select…</option>
              {tenants.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.external_tenant_id})</option>)}
            </select>
          </label>
          <button className="btn btn--primary btn--sm" type="submit">Add mapping</button>
        </form>

        {mappings.length === 0 ? (
          <p className="hint">No mappings. Events from this agent cannot be attributed to a tenant until one exists.</p>
        ) : (
          <table className="table">
            <thead><tr><th>External id</th><th>Tenant</th><th></th></tr></thead>
            <tbody>
              {mappings.map((m) => (
                <tr key={m.id}>
                  <td className="mono">{m.external_tenant_id}</td>
                  <td>{tenantName(m.tenant_id)} <span className="mono">{m.tenant_id}</span></td>
                  <td><button className="btn btn--sm btn--danger" onClick={() => removeMapping(m)}>Remove</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
