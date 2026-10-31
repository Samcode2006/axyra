import { Fragment, useCallback, useState } from 'react';
import { auditLogs, errMsg } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { ErrorMsg, Spinner } from '../../components/Feedback';
import { formatDate } from '../../lib/utils';
import type { AuditLog } from '../../types';

const PAGE = 25;

const RESOURCE_TYPES = [
  'organization', 'application', 'agent', 'agent_token', 'tenant',
  'tenant_mapping', 'policy', 'metric_definition', 'user',
];

/** Administrative change history. Customer admins see only their organization. */
export default function AuditLogPanel() {
  const [rows, setRows] = useState<AuditLog[]>([]);
  const [next, setNext] = useState<number | undefined>();
  const [resourceType, setResourceType] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<number | null>(null);

  const load = useCallback(
    async (beforeId?: number) => {
      setLoading(true);
      try {
        const res = await auditLogs.list({
          limit: PAGE,
          before_id: beforeId,
          resource_type: resourceType || undefined,
        });
        setRows((prev) => (beforeId ? [...prev, ...res.data.data] : res.data.data));
        setNext(res.data.next_before_id);
        setError(null);
      } catch (err) {
        setError(errMsg(err, 'Failed to load audit log'));
      } finally {
        setLoading(false);
      }
    },
    [resourceType]
  );

  useLoadEffect(load);

  return (
    <section className="panel">
      <div className="panel-head">
        <h2 className="panel-title">Audit log</h2>
        <select className="select-sm" value={resourceType} onChange={(e) => setResourceType(e.target.value)} aria-label="Filter by resource type">
          <option value="">All resources</option>
          {RESOURCE_TYPES.map((r) => <option key={r} value={r}>{r.replace('_', ' ')}</option>)}
        </select>
      </div>

      {error && <ErrorMsg message={error} />}

      {rows.length === 0 && !loading ? (
        <p className="hint">No administrative changes recorded.</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>When</th><th>Who</th><th>Action</th><th>Resource</th><th></th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <Fragment key={r.id}>
                <tr>
                  <td>{formatDate(r.created_at)}</td>
                  <td>{r.actor_name} <span className="badge badge--gray">{r.actor_role}</span></td>
                  <td className="mono">{r.action}</td>
                  <td className="mono">{r.resource_id}</td>
                  <td>
                    <button className="btn btn--sm" onClick={() => setOpen(open === r.id ? null : r.id)}>
                      {open === r.id ? 'Hide' : 'Details'}
                    </button>
                  </td>
                </tr>
                {open === r.id && (
                  <tr>
                    <td colSpan={5}><pre className="mono" style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(r.details, null, 2)}</pre></td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      )}

      {loading && <Spinner />}
      {next !== undefined && !loading && (
        <div className="row-actions" style={{ marginTop: 16 }}>
          <button className="btn btn--sm" onClick={() => void load(next)}>Load older</button>
        </div>
      )}
    </section>
  );
}
