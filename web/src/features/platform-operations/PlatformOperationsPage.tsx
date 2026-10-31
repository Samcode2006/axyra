import { Fragment, useCallback, useState } from 'react';
import { errMsg, organizations as api } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import type { Organization, User } from '../../types';

/** Operator-only: onboard customer organizations and their first administrators. */
export default function PlatformOperationsPage() {
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [users, setUsers] = useState<Record<string, User[]>>({});
  const [open, setOpen] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [orgName, setOrgName] = useState('');
  const [userForm, setUserForm] = useState({ username: '', password: '' });

  const load = useCallback(async () => {
    try {
      setOrgs((await api.list()).data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load organizations'));
    } finally {
      setLoading(false);
    }
  }, []);

  useLoadEffect(load);

  const loadUsers = async (orgId: string) => {
    const res = await api.users(orgId);
    setUsers((prev) => ({ ...prev, [orgId]: res.data }));
  };

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setError(null);
    } catch (err) {
      setError(errMsg(err));
    }
  };

  const toggleOrg = (id: string) => {
    if (open === id) return setOpen(null);
    setOpen(id);
    setUserForm({ username: '', password: '' });
    void run(() => loadUsers(id));
  };

  const createOrg = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await api.create({ name: orgName });
      setOrgName('');
      await load();
    });
  };

  const rename = (o: Organization) => {
    const name = prompt('New organization name', o.name)?.trim();
    if (name && name !== o.name) void run(async () => { await api.update(o.id, { name }); await load(); });
  };

  const removeOrg = (o: Organization) => {
    if (!confirm(`Delete "${o.name}" and ALL of its applications, agents, tenants, policies and users? This cannot be undone.`)) return;
    if (prompt(`Type the organization name to confirm:`) !== o.name) return;
    void run(async () => { await api.remove(o.id); setOpen(null); await load(); });
  };

  const createUser = (e: React.FormEvent, orgId: string) => {
    e.preventDefault();
    void run(async () => {
      await api.createUser(orgId, userForm);
      setUserForm({ username: '', password: '' });
      await loadUsers(orgId);
    });
  };

  const removeUser = (orgId: string, u: User) => {
    if (!confirm(`Delete user ${u.username}?`)) return;
    void run(async () => { await api.removeUser(orgId, u.id); await loadUsers(orgId); });
  };

  if (loading) return <Spinner />;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Platform Operations</h1>
      </div>

      {error && <ErrorMsg message={error} />}

      <section className="panel">
        <h2 className="panel-title">Customer organizations</h2>
        <form className="inline-form" onSubmit={createOrg}>
          <label>Organization name
            <input required value={orgName} onChange={(e) => setOrgName(e.target.value)} placeholder="Acme Retail" />
          </label>
          <button className="btn btn--primary btn--sm" type="submit">Create organization</button>
        </form>

        {orgs.length === 0 ? (
          <EmptyState message="No organizations." />
        ) : (
          <table className="table">
            <thead><tr><th>Name</th><th>Id</th><th>Created</th><th></th></tr></thead>
            <tbody>
              {orgs.map((o) => (
                <Fragment key={o.id}>
                  <tr>
                    <td>{o.name}</td>
                    <td className="mono">{o.id}</td>
                    <td>{new Date(o.created_at).toLocaleDateString()}</td>
                    <td>
                      <div className="row-actions">
                        <button className="btn btn--sm" onClick={() => toggleOrg(o.id)}>{open === o.id ? 'Close' : 'Users'}</button>
                        <button className="btn btn--sm" onClick={() => rename(o)}>Rename</button>
                        <button className="btn btn--sm btn--danger" onClick={() => removeOrg(o)}>Delete</button>
                      </div>
                    </td>
                  </tr>
                  {open === o.id && (
                    <tr>
                      <td colSpan={4}>
                        <div className="subpanel" style={{ marginTop: 0, borderTop: 'none' }}>
                          <h3>Console administrators</h3>
                          <form className="inline-form" onSubmit={(e) => createUser(e, o.id)}>
                            <label>Username
                              <input required minLength={3} value={userForm.username} onChange={(e) => setUserForm({ ...userForm, username: e.target.value })} />
                            </label>
                            <label>Password (12+ characters)
                              <input required type="password" minLength={12} maxLength={128} autoComplete="new-password" value={userForm.password} onChange={(e) => setUserForm({ ...userForm, password: e.target.value })} />
                            </label>
                            <button className="btn btn--primary btn--sm" type="submit">Add administrator</button>
                          </form>
                          {(users[o.id] ?? []).length === 0 ? (
                            <p className="hint">No administrators yet. This organization cannot sign in until one is added.</p>
                          ) : (
                            <table className="table">
                              <tbody>
                                {(users[o.id] ?? []).map((u) => (
                                  <tr key={u.id}>
                                    <td>{u.username}</td>
                                    <td><span className="badge badge--gray">{u.role}</span></td>
                                    <td><button className="btn btn--sm btn--danger" onClick={() => removeUser(o.id, u)}>Remove</button></td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          )}
                        </div>
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
