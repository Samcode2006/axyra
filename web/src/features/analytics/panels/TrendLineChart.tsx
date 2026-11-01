import { formatCompactNumber } from '../../../lib/utils';
import type { CSSProperties } from 'react';

interface Point { bucket: string; value: number; secondary?: number }

export function TrendLineChart({ points, lines, label, interval, formatValue, secondaryLabel }: {
  points: Point[];
  lines?: Array<{ id: string; name: string; color: string; points: Point[] }>;
  label: string;
  interval: 'hour' | 'day' | 'week';
  secondaryLabel?: string;
  formatValue: (value: number) => string;
}) {
  const clean = (data: Point[]) => data.filter((p) => Number.isFinite(p.value) && Number.isFinite(Date.parse(p.bucket)))
    .sort((a, b) => Date.parse(a.bucket) - Date.parse(b.bucket));
  const sorted = clean(lines ? lines.flatMap((line) => line.points) : points);
  if (!sorted.length) return <p className="hint">No trend data for this period.</p>;
  const min = sorted.reduce((value, p) => Math.min(value, p.value, p.secondary ?? 0), 0);
  const max = sorted.reduce((value, p) => Math.max(value, p.value, p.secondary ?? 0), 1);
  const first = Date.parse(sorted[0].bucket);
  const last = Date.parse(sorted[sorted.length - 1].bucket);
  const x = (bucket: string) => last === first ? 450 : 80 + (Date.parse(bucket) - first) / (last - first) * 740;
  const y = (value: number) => 230 - (value - min) / (max - min) * 200;
  const dateLabel = (bucket: string) => new Date(bucket).toLocaleString('en-GB', {
    day: '2-digit', month: 'short', ...(interval === 'hour' ? { hour: '2-digit', minute: '2-digit' } as const : {}), timeZone: 'UTC',
  });
  const buckets = [...new Set(sorted.map((point) => point.bucket))];
  const ticks = [...new Set(Array.from({ length: Math.min(5, buckets.length) }, (_, i) =>
    Math.round(i * (buckets.length - 1) / Math.max(Math.min(5, buckets.length) - 1, 1))))];
  const series = lines ? lines.map((line) => ({ ...line, secondary: false, data: clean(line.points) })).filter((line) => line.data.length) : [
    { id: 'primary', name: label, color: undefined, secondary: false, data: sorted },
    ...(secondaryLabel ? [{ id: 'secondary', name: secondaryLabel, color: undefined, secondary: true, data: sorted.filter((p) => Number.isFinite(p.secondary)).map((p) => ({ ...p, value: p.secondary! })) }] : []),
  ];

  return (
    <div className="trend-line-chart">
      <p className="hint">{secondaryLabel ? `${label} / ${secondaryLabel}` : label} · UTC · Hover over a point for its exact value.</p>
      {(secondaryLabel || lines) && <div className="trend-legend">{series.map((s) => <span key={s.id}><i style={s.color ? { borderColor: s.color } : undefined} className={s.secondary ? 'trend-swatch trend-swatch--secondary' : 'trend-swatch'} />{s.name}</span>)}</div>}
      {buckets.length === 1 && <p className="hint">One recorded bucket. The horizontal guide marks its value, not activity on other dates.</p>}
      <div className="trend-line-scroll">
        <svg viewBox="0 0 900 280" role="img" aria-label={`${label}${secondaryLabel ? ` and ${secondaryLabel}` : ''}, ${interval} trend. Exact values are available in the table below.`}>
          <title>{label} trend</title>
          {[0, 1, 2, 3, 4].map((i) => {
            const value = min + (max - min) * i / 4;
            return <g key={i}>
              <line x1="80" x2="820" y1={y(value)} y2={y(value)} className="trend-gridline" />
              <text x="70" y={y(value) + 4} textAnchor="end">{formatCompactNumber(value)}</text>
            </g>;
          })}
          {series.map((s) => <g key={s.id} style={s.color ? { '--series-color': s.color } as CSSProperties : undefined} className={s.secondary ? 'trend-series trend-series--secondary' : 'trend-series'}>
            {s.data.length === 1 ? (
              <line x1="80" x2="820" y1={y(s.data[0].value)} y2={y(s.data[0].value)} className="trend-line trend-guide" />
            ) : <polyline points={s.data.map((p) => `${x(p.bucket)},${y(p.value)}`).join(' ')} className="trend-line" />}
            {s.data.map((p) => <circle key={p.bucket} cx={x(p.bucket)} cy={y(p.value)} r={sorted.length > 100 ? 2 : 4} className="trend-point">
              <title>{s.name} · {new Date(p.bucket).toISOString()}: {formatValue(p.value)}</title>
            </circle>)}
          </g>)}
          {ticks.map((index) => <text key={index} x={x(buckets[index])} y="258" textAnchor="middle">{dateLabel(buckets[index])}</text>)}
        </svg>
      </div>
      <details className="trend-data">
        <summary>View chart data</summary>
        <div className="trend-data-scroll">
          <table className="table">
            <thead><tr><th scope="col">Date / time (UTC)</th>{lines && <th scope="col">Tenant</th>}<th scope="col">{label}</th>{secondaryLabel && <th scope="col">{secondaryLabel}</th>}</tr></thead>
            <tbody>{lines ? series.flatMap((s) => s.data.map((p) => <tr key={`${s.id}:${p.bucket}`}><td>{new Date(p.bucket).toISOString()}</td><td>{s.name}</td><td>{formatValue(p.value)}</td></tr>)) : sorted.map((p) => <tr key={p.bucket}><td>{new Date(p.bucket).toISOString()}</td><td>{formatValue(p.value)}</td>{secondaryLabel && <td>{p.secondary != null ? formatValue(p.secondary) : '—'}</td>}</tr>)}</tbody>
          </table>
        </div>
      </details>
    </div>
  );
}
