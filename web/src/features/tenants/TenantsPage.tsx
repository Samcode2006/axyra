import { useEffect, useState } from 'react';
import { tenants as tenantsApi } from '../../lib/api';
import { Badge } from '../../components/Badge';
import { Spinner, ErrorMsg, EmptyState } from '../../components/Feedback';
import type { Tenant } from '../../types';

export default function TenantsPage() {
  const [list, setList] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ name: '', external_tenant_id: '', organization_id: '' });
  const [submitting, setSubmitting] = useState(false);

  const load = () => {
    setLoading(true);
    tenantsApi
      .list()
      .then((r) => setList(r.data.data ?? []))
      .catch(() => setError('Failed to load tenants'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      await tenantsApi.create(form);
      setShowForm(false);
      setForm({ name: '', external_tenant_id: '', organization_id: '' });
      load();
    } catch {
      alert('Failed to create tenant');
    } finally {
      setSubmitting(false);
    }
  };

  const handleStatusChange = async (id: string, status: Tenant['status']) => {
    await tenantsApi.update(id, { status });
    load();
  };

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Tenant Management</h1>
        <button className="btn btn--primary" onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : '+ Register Tenant'}
        </button>
      </div>

      {showForm && (
        <form className="panel form" onSubmit={handleCreate}>
          <h2 className="panel-title">New Tenant</h2>
          <div className="form-row">
            <label>Name
              <input
                required
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Store Alpha"
              />
            </label>
            <label>External Tenant ID
              <input
                required
                value={form.external_tenant_id}
                onChange={(e) => setForm({ ...form, external_tenant_id: e.target.value })}
                placeholder="store_001"
              />
            </label>
            <label>Organization ID
              <input
                required
                value={form.organization_id}
                onChange={(e) => setForm({ ...form, organization_id: e.target.value })}
                placeholder="org_..."
              />
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
                <th>Organization</th>
                <th>Status</th>
                <th>Registered</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {list.map((t) => (
                <tr key={t.id}>
                  <td>{t.name}</td>
                  <td className="mono">{t.external_tenant_id}</td>
                  <td className="mono">{t.organization_id}</td>
                  <td><Badge status={t.status} /></td>
                  <td>{new Date(t.created_at).toLocaleDateString()}</td>
                  <td>
                    <select
                      value={t.status}
                      onChange={(e) => handleStatusChange(t.id, e.target.value as Tenant['status'])}
                      className="select-sm"
                    >
                      <option value="active">Active</option>
                      <option value="suspended">Suspend</option>
                      <option value="inactive">Deactivate</option>
                    </select>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}

