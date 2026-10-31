import { useCallback, useState } from 'react';
import { auth, errMsg, metricDefinitions as api } from '../../lib/api';
import { useLoadEffect } from '../../lib/useLoadEffect';
import { Badge } from '../../components/Badge';
import { EmptyState, ErrorMsg, Spinner } from '../../components/Feedback';
import type { Aggregation, MetricConfig, MetricDefinition, MetricFilter } from '../../types';

// These lists mirror the control plane's whitelists. The server re-validates
// everything; metrics are declarative and can never carry SQL.
const AGGREGATIONS: Aggregation[] = ['count', 'sum', 'avg', 'min', 'max', 'count_distinct'];
const NUMERIC_FIELDS = ['amount'];
const DISTINCT_FIELDS = ['order_id', 'product_id', 'category', 'status'];
const DIMENSIONS = ['status', 'category', 'product_id', 'currency'];

const EMPTY = {
  id: '', name: '', description: '', aggregation: 'count' as Aggregation, field: '',
  groupBy: [] as string[], granularity: [] as Array<'hour' | 'day'>,
  filterField: '', filterOp: 'eq' as MetricFilter['op'], filterValue: '',
};

function describe(c: MetricConfig): string {
  const what = c.aggregation === 'count' ? 'count of orders' : `${c.aggregation} of ${c.field}`;
  const by = c.group_by?.length ? ` by ${c.group_by.join(', ')}` : '';
  const where = c.filters?.length
    ? ` where ${c.filters.map((f) => `${f.field} ${f.op} ${Array.isArray(f.value) ? `[${f.value.join(', ')}]` : f.value}`).join(' and ')}`
    : '';
  return what + by + where;
}

export default function MetricConfigPage() {
  const operator = auth.isOperator();
  const [list, setList] = useState<MetricDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState(EMPTY);
  const [editingOrg, setEditingOrg] = useState<string | undefined>();

  const load = useCallback(async () => {
    try {
      setList((await api.list()).data);
      setError(null);
    } catch (err) {
      setError(errMsg(err, 'Failed to load metric definitions'));
    } finally {
      setLoading(false);
    }
  }, []);

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

  const toggle = <T extends string>(arr: T[], v: T): T[] => (arr.includes(v) ? arr.filter((x) => x !== v) : [...arr, v]);

  const buildConfig = (): MetricConfig => {
    const config: MetricConfig = { source: 'orders', aggregation: form.aggregation };
    if (form.aggregation !== 'count') config.field = form.field;
    if (form.groupBy.length) config.group_by = form.groupBy;
    if (form.granularity.length) config.granularity = form.granularity;
    if (form.filterField && form.filterValue.trim()) {
      config.filters = [{
        field: form.filterField,
        op: form.filterOp,
        value: form.filterOp === 'in' ? form.filterValue.split(',').map((s) => s.trim()).filter(Boolean) : form.filterValue.trim(),
      }];
    }
    return config;
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    void run(async () => {
      await api.put(form.id, {
        name: form.name,
        description: form.description || undefined,
        config: buildConfig(),
        organization_id: editingOrg,
      });
      setShowForm(false);
      setForm(EMPTY);
      setEditingOrg(undefined);
    });
  };

  const edit = (m: MetricDefinition) => {
    const f = m.config.filters?.[0];
    setForm({
      id: m.id, name: m.name, description: m.description ?? '', aggregation: m.config.aggregation,
      field: m.config.field ?? '', groupBy: m.config.group_by ?? [], granularity: m.config.granularity ?? [],
      filterField: f?.field ?? '', filterOp: f?.op ?? 'eq',
      filterValue: f ? (Array.isArray(f.value) ? f.value.join(', ') : f.value) : '',
    });
    setEditingOrg(m.organization_id ?? undefined);
    setShowForm(true);
  };

  const setEnabled = (m: MetricDefinition, enabled: boolean) =>
    run(() => api.put(m.id, {
      name: m.name, description: m.description, config: m.config, enabled,
      organization_id: m.organization_id ?? undefined,
    }));

  const remove = (m: MetricDefinition) => {
    if (!confirm(`Delete metric "${m.name}"?`)) return;
    void run(() => api.remove(m.id, operator ? m.organization_id ?? undefined : undefined));
  };

  if (loading) return <Spinner />;

  const needsField = form.aggregation !== 'count';
  const fieldChoices = form.aggregation === 'count_distinct' ? DISTINCT_FIELDS : NUMERIC_FIELDS;

  return (
    <div className="page">
      <div className="page-header">
        <h1>Metric Configuration</h1>
        <button className="btn btn--primary" onClick={() => { setShowForm(!showForm); setForm(EMPTY); setEditingOrg(undefined); }}>
          {showForm ? 'Cancel' : '+ New Metric'}
        </button>
      </div>

      <p className="hint">
        Metrics are declarative: choose an aggregation, a field and optional grouping. No SQL or expressions are accepted.
      </p>

      {error && <ErrorMsg message={error} />}

      {showForm && (
        <form className="panel form" onSubmit={submit}>
          <h2 className="panel-title">{editingOrg !== undefined || list.some((m) => m.id === form.id) ? 'Edit Metric' : 'New Metric'}</h2>
          <div className="form-row">
            <label>Metric id
              <input required pattern="[a-z][a-z0-9_]{2,63}" value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="revenue_by_category" />
              <span className="hint">Lowercase letters, digits and underscores.</span>
            </label>
            <label>Name
              <input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Revenue by category" />
            </label>
            <label>Description
              <input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </label>
          </div>
          <div className="form-row">
            <label>Aggregation
              <select value={form.aggregation} onChange={(e) => setForm({ ...form, aggregation: e.target.value as Aggregation, field: '' })}>
                {AGGREGATIONS.map((a) => <option key={a} value={a}>{a}</option>)}
              </select>
            </label>
            {needsField && (
              <label>Field
                <select required value={form.field} onChange={(e) => setForm({ ...form, field: e.target.value })}>
                  <option value="">Select…</option>
                  {fieldChoices.map((f) => <option key={f} value={f}>{f}</option>)}
                </select>
              </label>
            )}
          </div>
          <div className="form-row">
            <div>
              <p className="hint">Group by (up to 2)</p>
              <div className="row-actions">
                {DIMENSIONS.map((d) => (
                  <button type="button" key={d} className={`btn btn--sm ${form.groupBy.includes(d) ? 'btn--active' : ''}`}
                    disabled={!form.groupBy.includes(d) && form.groupBy.length >= 2}
                    onClick={() => setForm({ ...form, groupBy: toggle(form.groupBy, d) })}>{d}</button>
                ))}
              </div>
            </div>
            <div>
              <p className="hint">Time granularity</p>
              <div className="row-actions">
                {(['hour', 'day'] as const).map((g) => (
                  <button type="button" key={g} className={`btn btn--sm ${form.granularity.includes(g) ? 'btn--active' : ''}`}
                    onClick={() => setForm({ ...form, granularity: toggle(form.granularity, g) })}>{g}</button>
                ))}
              </div>
            </div>
          </div>
          <div className="form-row">
            <label>Filter field (optional)
              <select value={form.filterField} onChange={(e) => setForm({ ...form, filterField: e.target.value })}>
                <option value="">No filter</option>
                {DIMENSIONS.map((d) => <option key={d} value={d}>{d}</option>)}
              </select>
            </label>
            {form.filterField && (
              <>
                <label>Operator
                  <select value={form.filterOp} onChange={(e) => setForm({ ...form, filterOp: e.target.value as MetricFilter['op'] })}>
                    <option value="eq">equals</option>
                    <option value="neq">does not equal</option>
                    <option value="in">is one of</option>
                  </select>
                </label>
                <label>Value
                  <input required value={form.filterValue} onChange={(e) => setForm({ ...form, filterValue: e.target.value })}
                    placeholder={form.filterOp === 'in' ? 'placed, shipped' : 'placed'} />
                </label>
              </>
            )}
          </div>
          <button type="submit" className="btn btn--primary">Save metric</button>
        </form>
      )}

      {list.length === 0 ? (
        <EmptyState message="No metric definitions." />
      ) : (
        <section className="panel">
          <table className="table">
            <thead>
              <tr><th>Metric</th><th>Definition</th><th>Scope</th><th>Status</th><th></th></tr>
            </thead>
            <tbody>
              {list.map((m) => (
                <tr key={`${m.organization_id ?? 'builtin'}:${m.id}`}>
                  <td>{m.name}<div className="mono">{m.id}</div></td>
                  <td>{describe(m.config)}</td>
                  <td>{m.builtin ? <span className="badge badge--blue">built-in</span> : <span className="badge badge--gray">custom</span>}</td>
                  <td><Badge status={m.enabled ? 'active' : 'inactive'} /></td>
                  <td>
                    <div className="row-actions">
                      {(!m.builtin || operator) && <button className="btn btn--sm" onClick={() => edit(m)}>Edit</button>}
                      {(!m.builtin || operator) && (
                        <button className="btn btn--sm" onClick={() => void setEnabled(m, !m.enabled)}>{m.enabled ? 'Disable' : 'Enable'}</button>
                      )}
                      {!m.builtin && <button className="btn btn--sm btn--danger" onClick={() => remove(m)}>Delete</button>}
                    </div>
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
