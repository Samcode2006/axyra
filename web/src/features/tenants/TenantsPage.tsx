import { useCallback, useState } from 'react';
import { auth, errMsg, organizations as orgsApi, tenants as tenantsApi } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { Badge } from '../../components/Badge';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import { formatNumber } from '../../lib/utils';
import type { Organization, Tenant } from '../../types';

const PER_PAGE = 25;

export default function TenantsPage() {
  const operator = auth.isOperator();
  const [list, setList] = useState<Tenant[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [quotaForm, setQuotaForm] = useState({ events: '', queries: '' });
  const [form, setForm] = useState({ name: '', external_tenant_id: '', organization_id: '', events: '', queries: '' });
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    try {
      const [t, o] = await Promise.all([tenantsApi.list({ page, per_page: PER_PAGE }), orgsApi.list()]);
      setList(t.data.data ?? []);
      setTotal(t.data.total);
      setOrgs(o.data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load tenants'));
    } finally {
      setLoading(false);
    }
  }, [page]);

  useLoadEffect(load);

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setError(null);
      await load();
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const num = (v: string) => (v.trim() === '' ? undefined : Number(v));

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    await run(async () => {
      const events = num(form.events);
      const queries = num(form.queries);
      await tenantsApi.create({
        name: form.name,
        external_tenant_id: form.external_tenant_id,
        organization_id: operator ? form.organization_id : undefined,
        quota: events || queries ? { max_events_per_day: events, max_concurrent_queries: queries } : undefined,
      });
      setShowForm(false);
      setForm({ name: '', external_tenant_id: '', organization_id: '', events: '', queries: '' });
    });
    setSubmitting(false);
  };

  const startEdit = (t: Tenant) => {
    setEditing(t.id);
    setQuotaForm({
      events: String(t.quota?.max_events_per_day ?? ''),
      queries: String(t.quota?.max_concurrent_queries ?? ''),
    });
  };

  const saveQuota = (t: Tenant) =>
    run(async () => {
      await tenantsApi.update(t.id, {
        quota: { max_events_per_day: num(quotaForm.events), max_concurrent_queries: num(quotaForm.queries) },
      });
      setEditing(null);
    });

  const remove = (t: Tenant) => {
    if (!confirm(`Delete tenant "${t.name}" with its mappings and policies?`)) return;
    void run(() => tenantsApi.remove(t.id));
  };

  const pages = Math.max(1, Math.ceil(total / PER_PAGE));
  const orgName = (id: string) => orgs.find((o) => o.id === id)?.name ?? id;

  if (loading) return <Spinner />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Tenant Management</h1>
        <button className="btn btn--primary" onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : '+ Register Tenant'}
        </button>
      </div>

      {error && <ErrorMsg message={error} />}

      {showForm && (
        <form className="panel form" onSubmit={handleCreate}>
          <h2 className="panel-title">New Tenant</h2>
          <div className="form-row">
            <label>Name
              <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Store Alpha" />
            </label>
            <label>External Tenant ID
              <input required value={form.external_tenant_id} onChange={(e) => setForm({ ...form, external_tenant_id: e.target.value })} placeholder="store_001" />
              <span className="hint">The value in the tenant column of the source database.</span>
            </label>
            {operator && (
              <label>Organization
                <select required value={form.organization_id} onChange={(e) => setForm({ ...form, organization_id: e.target.value })}>
                  <option value="">Select…</option>
                  {orgs.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
                </select>
              </label>
            )}
          </div>
          <div className="form-row">
            <label>Max events per day
              <input type="number" min={1} value={form.events} onChange={(e) => setForm({ ...form, events: e.target.value })} placeholder="1000000" />
            </label>
            <label>Max concurrent queries
              <input type="number" min={1} max={1000} value={form.queries} onChange={(e) => setForm({ ...form, queries: e.target.value })} placeholder="10" />
            </label>
          </div>
          <button type="submit" className="btn btn--primary" disabled={submitting}>
            {submitting ? 'Creating…' : 'Create'}
          </button>
        </form>
      )}

      {list.length === 0 ? (
        <EmptyState message="No tenants registered yet." />
      ) : (
        <section className="panel">
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>External ID</th>
                {operator && <th>Organization</th>}
                <th>Status</th>
                <th>Quota</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {list.map((t) => (
                <tr key={t.id}>
                  <td>{t.name}<div className="mono">{t.id}</div></td>
                  <td className="mono">{t.external_tenant_id}</td>
                  {operator && <td>{orgName(t.organization_id)}</td>}
                  <td><Badge status={t.status} /></td>
                  <td>
                    {editing === t.id ? (
                      <div className="inline-form">
                        <label>Events/day
                          <input type="number" min={1} value={quotaForm.events} onChange={(e) => setQuotaForm({ ...quotaForm, events: e.target.value })} />
                        </label>
                        <label>Concurrent queries
                          <input type="number" min={1} max={1000} value={quotaForm.queries} onChange={(e) => setQuotaForm({ ...quotaForm, queries: e.target.value })} />
                        </label>
                        <button className="btn btn--primary btn--sm" onClick={() => void saveQuota(t)}>Save</button>
                        <button className="btn btn--sm" onClick={() => setEditing(null)}>Cancel</button>
                      </div>
                    ) : (
                      <>
                        {t.quota ? `${formatNumber(t.quota.max_events_per_day)} events/day · ${t.quota.max_concurrent_queries} queries` : '—'}{' '}
                        <button className="btn btn--sm" onClick={() => startEdit(t)}>Edit</button>
                      </>
                    )}
                  </td>
                  <td>
                    <div className="row-actions">
                      <select
                        value={t.status}
                        onChange={(e) => void run(() => tenantsApi.update(t.id, { status: e.target.value as Tenant['status'] }))}
                        className="select-sm"
                      >
                        <option value="active">Active</option>
                        <option value="suspended">Suspend</option>
                        <option value="inactive">Deactivate</option>
                      </select>
                      <button className="btn btn--sm btn--danger" onClick={() => remove(t)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {pages > 1 && (
            <div className="row-actions" style={{ marginTop: 16 }}>
              <button className="btn btn--sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</button>
              <span className="hint">Page {page} of {pages} · {total} tenants</span>
              <button className="btn btn--sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
