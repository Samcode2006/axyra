import { useCallback, useState } from 'react';
import { errMsg, policies as policiesApi, tenants as tenantsApi } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import type { Policy, PolicyType, Tenant } from '../../types';
import AuditLogPanel from './AuditLogPanel';

/** Config fields per policy type. These mirror the control plane's validation. */
const POLICY_FIELDS: Record<PolicyType, Array<{ key: string; label: string; required?: boolean; list?: boolean }>> = {
  rate_limit: [{ key: 'max_events_per_minute', label: 'Max events per minute', required: true }],
  query_limit: [
    { key: 'max_concurrent_queries', label: 'Max concurrent queries', required: true },
    { key: 'max_queries_per_minute', label: 'Max queries per minute' },
  ],
  data_access: [
    { key: 'max_lookback_days', label: 'Max lookback (days)' },
    { key: 'allowed_metrics', label: 'Allowed metric ids (comma separated)', list: true },
  ],
};

const TYPE_LABEL: Record<PolicyType, string> = {
  rate_limit: 'Rate limit',
  query_limit: 'Query limit',
  data_access: 'Data access',
};

function buildConfig(type: PolicyType, values: Record<string, string>): Record<string, unknown> {
  const config: Record<string, unknown> = {};
  for (const f of POLICY_FIELDS[type]) {
    const raw = (values[f.key] ?? '').trim();
    if (!raw) continue;
    config[f.key] = f.list ? raw.split(',').map((s) => s.trim()).filter(Boolean) : Number(raw);
  }
  return config;
}

function summarize(config: Record<string, unknown>): string {
  return Object.entries(config)
    .map(([k, v]) => `${k.replace(/_/g, ' ')}: ${Array.isArray(v) ? v.join(', ') : String(v)}`)
    .join(' · ');
}

export default function AccessPoliciesPage() {
  const [list, setList] = useState<Policy[]>([]);
  const [tenantList, setTenantList] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [type, setType] = useState<PolicyType>('rate_limit');
  const [form, setForm] = useState({ tenant_id: '', name: '' });
  const [values, setValues] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [auditKey, setAuditKey] = useState(0);

  const load = useCallback(async () => {
    try {
      const [p, t] = await Promise.all([policiesApi.list(), tenantsApi.list({ per_page: 200 })]);
      setList(p.data);
      setTenantList(t.data.data ?? []);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load policies'));
    } finally {
      setLoading(false);
    }
  }, []);

  useLoadEffect(load);

  const refresh = async () => {
    await load();
    setAuditKey((k) => k + 1); // remount the audit panel so the change shows up
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      await policiesApi.create({ ...form, type, config: buildConfig(type, values) });
      setShowForm(false);
      setForm({ tenant_id: '', name: '' });
      setValues({});
      await refresh();
    } catch (err) {
      setError(errMsg(err, 'Failed to create policy'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleEdit = async (p: Policy) => {
    const raw = prompt('Policy config (JSON)', JSON.stringify(p.config));
    if (raw === null) return;
    try {
      await policiesApi.update(p.id, { config: JSON.parse(raw) as Record<string, unknown> });
      await refresh();
    } catch (err) {
      setError(err instanceof SyntaxError ? 'That is not valid JSON' : errMsg(err));
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this policy?')) return;
    try {
      await policiesApi.delete(id);
      await refresh();
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const tenantName = (id: string) => tenantList.find((t) => t.id === id)?.name ?? id;

  if (loading) return <Spinner />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Access & Policies</h1>
        <button className="btn btn--primary" onClick={() => setShowForm(!showForm)} disabled={tenantList.length === 0}>
          {showForm ? 'Cancel' : '+ New Policy'}
        </button>
      </div>

      {error && <ErrorMsg message={error} />}

      {showForm && (
        <form className="panel form" onSubmit={handleCreate}>
          <h2 className="panel-title">New Policy</h2>
          <div className="form-row">
            <label>Tenant
              <select required value={form.tenant_id} onChange={(e) => setForm({ ...form, tenant_id: e.target.value })}>
                <option value="">Select…</option>
                {tenantList.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
              </select>
            </label>
            <label>Policy Name
              <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Ingestion cap" />
            </label>
            <label>Type
              <select value={type} onChange={(e) => { setType(e.target.value as PolicyType); setValues({}); }}>
                {(Object.keys(TYPE_LABEL) as PolicyType[]).map((k) => <option key={k} value={k}>{TYPE_LABEL[k]}</option>)}
              </select>
            </label>
          </div>
          <div className="form-row">
            {POLICY_FIELDS[type].map((f) => (
              <label key={f.key}>{f.label}{f.required ? ' *' : ''}
                <input
                  required={f.required}
                  type={f.list ? 'text' : 'number'}
                  min={f.list ? undefined : 1}
                  value={values[f.key] ?? ''}
                  onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                />
              </label>
            ))}
          </div>
          {type === 'data_access' && <p className="hint">Set a lookback limit, an allow-list of metrics, or both.</p>}
          <button type="submit" className="btn btn--primary" disabled={submitting}>
            {submitting ? 'Creating…' : 'Create'}
          </button>
        </form>
      )}

      {list.length === 0 ? (
        <EmptyState message="No policies configured yet." />
      ) : (
        <section className="panel">
          <table className="table">
            <thead>
              <tr><th>Name</th><th>Type</th><th>Tenant</th><th>Limits</th><th></th></tr>
            </thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id}>
                  <td>{p.name}</td>
                  <td>{TYPE_LABEL[p.type] ?? p.type}</td>
                  <td>{tenantName(p.tenant_id)}</td>
                  <td className="mono">{summarize(p.config)}</td>
                  <td>
                    <div className="row-actions">
                      <button className="btn btn--sm" onClick={() => void handleEdit(p)}>Edit</button>
                      <button className="btn btn--sm btn--danger" onClick={() => void handleDelete(p.id)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      <AuditLogPanel key={auditKey} />
    </div>
  );
}
