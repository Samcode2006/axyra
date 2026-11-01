import { useCallback, useEffect, useState } from 'react';
import { usePanel } from '../analytics/panels/usePanel';
import { auth, errMsg, organizations as orgsApi, tenants as tenantsApi } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { Badge } from '../../components/Badge';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import { formatNumber } from '../../lib/utils';
import type { Organization, Tenant } from '../../types';

const PER_PAGE = 25;

const REGIONS = ['global', 'apac', 'emea', 'americas'] as const;
const PLANS = ['free', 'growth', 'enterprise'] as const;
const CYCLES = ['monthly', 'annual'] as const;

const EMPTY_FORM = {
  database_mode: 'existing', name: '', external_tenant_id: '', organization_id: '', events: '', queries: '',
  region: 'global', plan_tier: 'growth', billing_cycle: 'monthly', tags: '',
};

/** "vip, eu-west" -> ["vip", "eu-west"]. The server also trims, lowercases and de-duplicates. */
const parseTags = (s: string) => s.split(',').map((t) => t.trim()).filter(Boolean);

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
  const [form, setForm] = useState({ ...EMPTY_FORM });
  const [profileEdit, setProfileEdit] = useState<string | null>(null);
  const [profile, setProfile] = useState({ region: 'global', plan_tier: 'growth', billing_cycle: 'monthly', tags: '' });
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
        database_mode: form.database_mode as 'existing' | 'managed',
        name: form.name,
        external_tenant_id: form.external_tenant_id,
        organization_id: operator ? form.organization_id : undefined,
        quota: events || queries ? { max_events_per_day: events, max_concurrent_queries: queries } : undefined,
        region: form.region as Tenant['region'],
        plan_tier: form.plan_tier as Tenant['plan_tier'],
        billing_cycle: form.billing_cycle as Tenant['billing_cycle'],
        tags: parseTags(form.tags),
      });
      setShowForm(false);
      setForm({ ...EMPTY_FORM });
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

  const startProfile = (t: Tenant) => {
    setProfileEdit(t.id);
    setProfile({ region: t.region, plan_tier: t.plan_tier, billing_cycle: t.billing_cycle, tags: t.tags.join(', ') });
  };

  const saveProfile = (t: Tenant) =>
    run(async () => {
      await tenantsApi.update(t.id, {
        region: profile.region as Tenant['region'],
        plan_tier: profile.plan_tier as Tenant['plan_tier'],
        billing_cycle: profile.billing_cycle as Tenant['billing_cycle'],
        tags: parseTags(profile.tags),
      });
      setProfileEdit(null);
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
          <label>Database
            <select value={form.database_mode} onChange={(e) => setForm({ ...form, database_mode: e.target.value })}>
              <option value="existing">Connect an existing database</option>
              <option value="managed">Create a managed PostgreSQL database</option>
            </select>
          </label>
          <p className="hint">{form.database_mode === 'managed' ? 'Creates a private database and dedicated agent. Provisioning continues in the background. Database volumes are retained; backups and removal require an operator.' : 'Registers the tenant. Configure an agent separately to connect your existing database.'}</p>
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
          <div className="form-row">
            <label>Region
              <select value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })}>
                {REGIONS.map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </label>
            <label>Plan
              <select value={form.plan_tier} onChange={(e) => setForm({ ...form, plan_tier: e.target.value })}>
                {PLANS.map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </label>
            <label>Billing
              <select value={form.billing_cycle} onChange={(e) => setForm({ ...form, billing_cycle: e.target.value })}>
                {CYCLES.map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </label>
            <label>Tags
              <input value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} placeholder="vip, eu-west" />
              <span className="hint">Comma separated, up to 20.</span>
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
                <th>Plan</th>
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
                  <td><Badge status={t.status} /><Provisioning tenant={t} onReady={load} /></td>
                  <td>
                    {profileEdit === t.id ? (
                      <div className="inline-form">
                        <label>Region
                          <select value={profile.region} onChange={(e) => setProfile({ ...profile, region: e.target.value })}>
                            {REGIONS.map((r) => <option key={r} value={r}>{r}</option>)}
                          </select>
                        </label>
                        <label>Plan
                          <select value={profile.plan_tier} onChange={(e) => setProfile({ ...profile, plan_tier: e.target.value })}>
                            {PLANS.map((r) => <option key={r} value={r}>{r}</option>)}
                          </select>
                        </label>
                        <label>Billing
                          <select value={profile.billing_cycle} onChange={(e) => setProfile({ ...profile, billing_cycle: e.target.value })}>
                            {CYCLES.map((r) => <option key={r} value={r}>{r}</option>)}
                          </select>
                        </label>
                        <label>Tags
                          <input value={profile.tags} onChange={(e) => setProfile({ ...profile, tags: e.target.value })} />
                        </label>
                        <button className="btn btn--primary btn--sm" onClick={() => void saveProfile(t)}>Save</button>
                        <button className="btn btn--sm" onClick={() => setProfileEdit(null)}>Cancel</button>
                      </div>
                    ) : (
                      <>
                        {t.plan_tier}  - {t.billing_cycle}
                        <div className="hint">{t.region}{t.tags.length > 0 && `  - ${t.tags.join(', ')}`}</div>
                        <button className="btn btn--sm" onClick={() => startProfile(t)}>Edit</button>
                      </>
                    )}
                  </td>
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
                        {t.quota ? `${formatNumber(t.quota.max_events_per_day)} events/day  - ${t.quota.max_concurrent_queries} queries` : '—'}{' '}
                        <button className="btn btn--sm" onClick={() => startEdit(t)}>Edit</button>
                      </>
                    )}
                  </td>
                  <td>
                    <div className="row-actions">
                      <select
                        disabled={t.status === 'provisioning'}
                        value={t.status}
                        onChange={(e) => void run(() => tenantsApi.update(t.id, { status: e.target.value as Tenant['status'] }))}
                        className="select-sm"
                      >
                        {t.status === 'provisioning' && <option value="provisioning">Provisioning</option>}
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
              <span className="hint">Page {page} of {pages}  - {total} tenants</span>
              <button className="btn btn--sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
            </div>
          )}
        </section>
      )}
    </div>
  );
}

const stages: Record<string,string> = { queued:'Queued', creating_database:'Creating database', preparing_schema:'Preparing schema', registering_agent:'Connecting agent', waiting_for_stream:'Verifying live stream', ready:'Ready' };
function Provisioning({tenant,onReady}:{tenant:Tenant;onReady:()=>Promise<void>}) {
 const live=usePanel(()=>tenantsApi.provisioning(tenant.id).then(r=>r.data),`provision|${tenant.id}`,3000);
 const [retryError,setRetryError]=useState<string|null>(null);
 const [retrying,setRetrying]=useState(false);
 const p=live.data;
 useEffect(()=>{if(p?.status==='ready' && tenant.status==='provisioning') void onReady();},[p?.status,tenant.status,onReady]);
 if(live.error) return <span className="hint">Provisioning status unavailable</span>;
 if(!p || p.status==='external') return null;
 return <div className="hint">
  <div>Managed database ? {p.status==='failed' ? 'Failed' : stages[p.stage ?? 'queued'] ?? p.stage}</div>
  {p.status==='ready' && <div title="Private Docker network address">{p.host}:{p.port}/{p.database}</div>}
  {p.error && <div role="alert">{p.error}</div>}
  {retryError && <div role="alert">{retryError}</div>}
  {p.status==='failed' && <button className="btn btn--sm" disabled={retrying} onClick={async()=>{
   setRetrying(true);setRetryError(null);
   try{await tenantsApi.retryProvisioning(tenant.id);live.reload();}catch(e){setRetryError(errMsg(e));}finally{setRetrying(false);}
  }}>{retrying?'Retrying?':'Retry provisioning'}</button>}
 </div>;
}
