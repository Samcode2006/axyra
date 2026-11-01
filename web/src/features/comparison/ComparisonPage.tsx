import { useEffect, useRef, useState } from 'react';
import { Check, Database, Download, GitCompareArrows, Play, RefreshCw, Square } from 'lucide-react';
import { errMsg } from '../../lib/api';
import { comparison, type Product, type Sample, type Source, type Ticket, type Window } from './api';
import './comparison.css';

const stages = ['Check baseline', 'Create O1 · 1,000', 'Create O2 · 500', 'Create O3 · 2,000', 'Update O2 · 750', 'Cancel O2'];
type Stage = { state: 'pending' | 'running' | 'passed' | 'failed'; elapsed?: number; sample?: Sample };
const initialStages = (): Stage[] => stages.map(() => ({ state: 'pending' }));
const money = (n: number) => n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const delay = (signal: AbortSignal) => new Promise<void>((resolve, reject) => {
  const abort = () => { clearTimeout(timer); reject(new DOMException('Stopped', 'AbortError')); };
  const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, 1500);
  signal.addEventListener('abort', abort, { once: true });
  if (signal.aborted) abort();
});

function Results({ title, rows, ms, empty }: { title: string; rows?: Product[]; ms?: number; empty: string }) {
  const revenue = rows?.reduce((sum, row) => sum + row.revenue, 0) ?? 0;
  return <section className="compare-result">
    <div className="compare-result-heading"><h2>{title}</h2><span>{ms === undefined ? '—' : `${ms.toFixed(2)} ms DB query`}</span></div>
    <div className="compare-total">{rows ? money(revenue) : '—'}<small>Revenue in returned products</small></div>
    <div className="compare-table-scroll"><table><thead><tr><th>Product</th><th>Orders</th><th>Revenue</th></tr></thead>
      <tbody>{rows?.map(row => <tr key={row.product_id}><td title={row.product_id}>{row.product_id}</td><td>{row.order_count}</td><td>{money(row.revenue)}</td></tr>)}</tbody>
    </table></div>
    {!rows?.length && <p className="compare-empty">{rows ? 'No products in this window.' : empty}</p>}
  </section>;
}

export default function ComparisonPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [loading, setLoading] = useState(true);
  const [tenant, setTenant] = useState('');
  const [mode, setMode] = useState<'all' | 'range'>('all');
  const [from, setFrom] = useState(new Date().toISOString().slice(0, 10));
  const [to, setTo] = useState(new Date().toISOString().slice(0, 10));
  const [sample, setSample] = useState<Sample>();
  const [error, setError] = useState('');
  const [message, setMessage] = useState('Choose a source and compare real results.');
  const [busy, setBusy] = useState<'live' | 'once' | 'simulation' | ''>('');
  const [steps, setSteps] = useState<Stage[]>(initialStages);
  const [ticket, setTicket] = useState<Ticket>();
  const [consent, setConsent] = useState(false);
  const [loadKey, setLoadKey] = useState(0);
  const active = useRef<AbortController | null>(null);
  const selected = sources.find(s => s.id === tenant);
  const validRange = mode === 'all' || (!!from && !!to && from <= to);
  const ready = selected?.configured && validRange && !busy;

  useEffect(() => {
    const controller = new AbortController();
    comparison.sources(controller.signal).then(({ data }) => {
      setSources(data); setTenant(data.find(s => s.configured)?.id ?? data[0]?.id ?? '');
    }).catch(e => { if (!controller.signal.aborted) setError(errMsg(e)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => { controller.abort(); active.current?.abort(); };
  }, [loadKey]);

  function reset() { setSample(undefined); setTicket(undefined); setSteps(initialStages()); setError(''); setMessage('Ready to compare.'); }
  function start(kind: typeof busy) {
    active.current?.abort(); const controller = new AbortController(); active.current = controller;
    setBusy(kind); setError(''); return controller;
  }
  function finish(controller: AbortController) { if (active.current === controller) { setBusy(''); active.current = null; } }
  async function read(window: Window, controller: AbortController) {
    const { data } = await comparison.snapshot(tenant, window, controller.signal);
    if (controller.signal.aborted) throw new DOMException('Stopped', 'AbortError');
    setSample(data); return data;
  }
  async function compare(live: boolean) {
    setTicket(undefined); setSteps(initialStages()); setSample(undefined);
    const controller = start(live ? 'live' : 'once');
    const window: Window = mode === 'all' ? {} : { from: `${from}T00:00:00Z`, to: `${to}T23:59:59.999999Z` };
    try {
      do {
        if (live && document.hidden) { await delay(controller.signal); continue; }
        const data = await read(window, controller);
        setMessage(data.match ? 'Source SQL and Axyra match in this sample.' : data.source_stable ? 'Results differ. Axyra may still be processing.' : 'Source changed during the check. Waiting for a stable sample.');
        if (!live) break;
        await delay(controller.signal);
      } while (!controller.signal.aborted);
    } catch (e) { if (!controller.signal.aborted) { setError(errMsg(e)); setMessage('Comparison could not finish.'); } }
    finally { finish(controller); }
  }
  async function simulate() {
    const controller = start('simulation'); setSteps(initialStages()); setSample(undefined); setTicket(undefined);
    let run: Ticket | undefined; let current = 0;
    try {
      for (current = 0; current < stages.length; current++) {
        const index = current; const started = performance.now();
        setSteps(prev => prev.map((s, i) => i === index ? { state: 'running' } : s));
        setMessage(`${stages[index]} — ${index ? 'committing to the source database' : 'checking replication readiness'}…`);
        run = (await comparison.step(tenant, index, run?.ticket ?? '', controller.signal)).data;
        if (controller.signal.aborted) throw new DOMException('Stopped', 'AbortError');
        setTicket(run);
        let streak = 0; let previous = ''; let passed = false;
        while (performance.now() - started < 90_000) {
          const data = await read({ from: run.from, to: run.to }, controller);
          // Ensure the expected fixture itself is visible, even with other traffic
          // or a top-100 cutoff; matching empty results must never pass a write.
          const a = data.source.find(p => p.product_id === `${run!.prefix}_A`);
          const b = data.source.find(p => p.product_id === `${run!.prefix}_B`);
          const expectedA = [0, 1000, 1500, 1500, 1750, 1000][index];
          const fixturesOK = index === 0 || (!!a && a.revenue === expectedA && a.order_count === (index === 1 || index === 5 ? 1 : 2) && (index < 3 || (!!b && b.revenue === 2000 && b.order_count === 1)));
          const signature = JSON.stringify(data.source);
          streak = data.match && fixturesOK ? (signature === previous ? streak + 1 : 1) : 0;
          previous = signature;
          setMessage(`${stages[index]} — waiting for capture and processing (${streak}/2 stable matches)…`);
          if (streak >= 2) {
            setSteps(prev => prev.map((s, i) => i === index ? { state: 'passed', elapsed: (performance.now() - started) / 1000, sample: data } : s));
            passed = true; break;
          }
          await delay(controller.signal);
        }
        if (!passed) throw new Error('Simulation timed out after 90 seconds. Check agent connectivity, mappings and dead-letter events.');
      }
      setMessage('Simulation complete. All six stages passed with two stable matching samples each.');
    } catch (e) {
      setSteps(prev => prev.map((s, i) => i === current ? { ...s, state: 'failed' } : s));
      if (!controller.signal.aborted) {
        setError(e instanceof Error && e.message.startsWith('Simulation timed out') ? e.message : errMsg(e));
        setMessage('Simulation stopped before completion. Completed writes are retained.');
      }
    } finally { finish(controller); }
  }
  function stop() { active.current?.abort(); setMessage('Stopped. Any committed simulation orders remain in the source.'); }
  function download() {
    const blob = new Blob([JSON.stringify({ tenant, source: selected?.store, mode: ticket ? 'simulation range' : mode, window: ticket ? { from: ticket.from, to: ticket.to } : mode === 'range' ? { from, to } : null, fixture_prefix: ticket?.prefix, stages: steps, latest: sample }, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = 'axyra-comparison.json'; a.click(); URL.revokeObjectURL(url);
  }
  return <div className="comparison-page">
    <header className="page-header"><div><p className="compare-eyebrow">VERIFY YOUR PIPELINE</p><h1>Live Comparison</h1><p className="hint">Real source SQL. Real Axyra results. Follow every change from database commit to analytics.</p></div>
      <button className="btn" disabled={!sample} onClick={download}><Download size={15} /> Export report</button></header>
    <div className="compare-flow" aria-label="Data flow">{['Source PostgreSQL', 'WAL capture', 'Agent & queue', 'Analytics worker', 'Axyra results'].map((label, i) => <span key={label}><b>{i + 1}</b>{label}{i < 4 && <i aria-hidden="true">→</i>}</span>)}</div>
    <section className="compare-controls">
      <label>Source tenant<select disabled={!!busy || loading} value={tenant} onChange={e => { setTenant(e.target.value); reset(); }}><option value="" disabled>{loading ? 'Loading tenants…' : 'Select tenant'}</option>{sources.map(s => <option key={s.id} value={s.id}>{s.name}{!s.configured ? ' · source not configured' : ''}</option>)}</select></label>
      <label>Query mode<select disabled={!!busy} value={mode} onChange={e => { setMode(e.target.value as typeof mode); reset(); }}><option value="all">All time · precomputed totals</option><option value="range">Date range · captured orders</option></select></label>
      {mode === 'range' && <><label>From (UTC)<input disabled={!!busy} type="date" value={from} onChange={e => { setFrom(e.target.value); reset(); }} /></label><label>To (UTC)<input disabled={!!busy} type="date" value={to} onChange={e => { setTo(e.target.value); reset(); }} /></label></>}
      <div className="compare-buttons"><button className="btn" disabled={!ready} onClick={() => void compare(false)}><RefreshCw size={15} /> Compare once</button><button className="btn btn--primary" disabled={!ready} onClick={() => void compare(true)}><Play size={15} /> Watch live</button>{busy && <button className="btn" onClick={stop}><Square size={14} /> Stop</button>}</div>
    </section>
    {!loading && !sources.length && !error && <p className="compare-notice">No accessible tenants yet. Register a tenant under Tenants to start.</p>}
    {selected && !selected.configured && <p className="compare-notice">This tenant needs a server-side source connection in AXYRA_COMPARISON_SOURCES. The reference Compose stores are configured automatically.</p>}
    {!validRange && <p role="alert" className="compare-notice">Choose a valid date range with the end on or after the start.</p>}
    {error && <div role="alert" className="compare-error">{error}{!sources.length && <button className="btn" onClick={() => { setError(''); setLoading(true); setLoadKey(k => k + 1); }}>Retry connection</button>}</div>}
    <div className="compare-status" role="status"><span className={`compare-indicator ${sample?.match ? 'matched' : ''}`} /><strong>{message}</strong><small>{sample ? `Last checked ${new Date(sample.checked_at).toLocaleTimeString()}` : 'No sample yet'}</small></div>
    {ticket && <p className="hint">Simulation window: {ticket.from} → {ticket.to}. Fixture prefix: <code>{ticket.prefix}</code>. The comparison below uses captured orders for this fixed creation window.</p>}
    <div className="compare-results"><Results title="Source database" rows={sample?.source} ms={sample?.source_ms} empty="Run a comparison to query source orders." /><div className="compare-versus"><GitCompareArrows size={24} /></div><Results title="Axyra analytics" rows={sample?.analytics} ms={sample?.analytics_ms} empty="The same tenant and window will be queried." /></div>
    {sample && (!sample.match) && <section className="compare-notice"><strong>{!sample.source_stable ? 'Source changed during sampling' : `${sample.differences.length} differences detected`}</strong><ul>{sample.differences.map((d, i) => <li key={i}>{d}</li>)}</ul></section>}
    <section className="compare-simulation"><div className="compare-simulation-heading"><div><h2><Database size={19} /> End-to-end simulation</h2><p>Three INR orders, one price update, one cancellation. Each stage waits for two stable matches.</p></div><button className="btn btn--primary" disabled={!selected?.configured || !!busy || !consent} onClick={() => void simulate()}><Play size={15} /> Run full simulation</button></div>
      <label className="compare-consent"><input type="checkbox" checked={consent} disabled={!!busy} onChange={e => setConsent(e.target.checked)} /> Create three test orders in this source store and retain them for inspection.</label>
      <ol className="compare-stages">{stages.map((label, i) => <li key={label} data-state={steps[i].state}><span>{steps[i].state === 'passed' ? <Check size={16} /> : i + 1}</span><div><strong>{label}</strong><small>{steps[i].state === 'passed' ? `Matched · ${steps[i].elapsed?.toFixed(1)}s` : steps[i].state}</small></div></li>)}</ol>
    </section>
    <details className="compare-method"><summary>What this comparison verifies</summary><p>Both reads compare the top 100 products by non-cancelled revenue, including order counts and ranking. Amounts are compared as exact decimals on the server. All-time queries read Axyra’s product aggregates; dated queries aggregate captured order state by original creation time. The simulation uses a fixed creation window; other orders in that window are included on both sides.</p><p>Both query timings measure Go database execution through reading the results. Source connection setup is completed before timing. Axyra reports its database duration directly, including any pool wait; internal HTTP, browser time and JSON encoding are excluded. Simulation stage times measure propagation and polling, not DB query time. Neither is a controlled benchmark. A match does not prove complete capture outside these products or validate every metric. Use a single-currency store. Live watching pauses in hidden tabs; stop it before changing the selection.</p><pre>{`SELECT product_id,\n  COUNT(*) FILTER (WHERE status <> 'cancelled'),\n  COALESCE(SUM(amount) FILTER (WHERE status <> 'cancelled'), 0) AS revenue\nFROM orders\nWHERE store_id = $1 AND product_id <> '' -- plus selected creation window\nGROUP BY product_id\nORDER BY revenue DESC, product_id\nLIMIT 100;`}</pre></details>
  </div>;
}
