import type { DBQueryTiming } from '../lib/useDBQueryTiming';

export function DBQueryTime({ timings, combined = false }: { timings: DBQueryTiming[]; combined?: boolean }) {
  const ready = timings.length > 0 && timings.every((timing) => timing.ms !== null);
  const ms = timings.reduce((sum, timing) => sum + (timing.ms ?? 0), 0);
  const orders = timings.reduce((sum, timing) => sum + timing.orders, 0);

  return (
    <div className="query-retrieval" title="Measured in Go from the aggregate DB call through scanning its result. Includes DB connection-pool wait and Go–DB communication. Excludes browser, proxy, JSON encoding and other backend work.">
      <strong>{combined ? 'Combined DB query time' : 'Summary DB query time'}</strong>
      <span className="mono">{ready ? `${ms.toFixed(2)} ms` : timings.length ? 'Unavailable' : 'Measuring…'}</span>
      {ready && <>
        <span>{orders.toLocaleString()} {combined ? 'active orders' : 'orders'} represented</span>
        <span className={`badge badge--${ms < 200 ? 'green' : 'amber'}`}>{ms < 200 ? 'Under 200 ms' : '200 ms or more'}</span>
      </>}
      <span className="hint">Precomputed aggregates{combined ? ` · sum of ${timings.length} tenant queries` : ''}</span>
    </div>
  );
}
