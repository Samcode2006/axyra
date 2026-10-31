import { useEffect, useState } from 'react';
import { policies as policiesApi } from '../../lib/api';
import { Spinner, ErrorMsg, EmptyState } from '../../components/Feedback';
import type { Policy } from '../../types';

export default function AccessPoliciesPage() {
  const [list, setList] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ tenant_id: '', name: '', type: 'rate_limit' as Policy['type'] });
  const [submitting, setSubmitting] = useState(false);

  const load = () => {
    setLoading(true);
    policiesApi
      .list()
      .then((r) => setList(r.data))
      .catch(() => setError('Failed to load policies'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      await policiesApi.create({ ...form, config: {} });
      setShowForm(false);
      load();
    } catch {
      alert('Failed to create policy');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this policy?')) return;
    await policiesApi.delete(id);
    load();
  };

  if (loading) return <Spinner />;
  if (error) return <ErrorMsg message={error} />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Access & Policies</h1>
        <button className="btn btn--primary" onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : '+ New Policy'}
        </button>
      </div>

      {showForm && (
        <form className="panel form" onSubmit={handleCreate}>
          <h2 className="panel-title">New Policy</h2>
          <div className="form-row">
            <label>Tenant ID
              <input required value={form.tenant_id} onChange={(e) => setForm({ ...form, tenant_id: e.target.value })} placeholder="tenant_..." />
            </label>
            <label>Policy Name
              <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Rate limit policy" />
            </label>
            <label>Type
              <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value as Policy['type'] })}>
                <option value="rate_limit">Rate Limit</option>
                <option value="query_limit">Query Limit</option>
                <option value="data_access">Data Access</option>
              </select>
            </label>
          </div>
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
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Tenant</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id}>
                  <td>{p.name}</td>
                  <td>{p.type}</td>
                  <td className="mono">{p.tenant_id}</td>
                  <td>{new Date(p.created_at).toLocaleDateString()}</td>
                  <td>
                    <button className="btn btn--sm btn--danger" onClick={() => handleDelete(p.id)}>
                      Delete
                    </button>
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

