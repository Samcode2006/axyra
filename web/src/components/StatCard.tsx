import { memo, useEffect, useRef, useState } from 'react';
import { cn, formatCompactCurrency, formatCompactNumber } from '../lib/utils';

interface StatCardProps {
  label: string;
  value: string | number;
  numericValue?: number;
  currency?: string;
  fullValue?: string;
  sub?: string;
  accent?: 'green' | 'blue' | 'amber' | 'red';
}

export const StatCard = memo(function StatCard({ label, value, numericValue, currency, fullValue, sub, accent }: StatCardProps) {
  return (
    <div className={cn('stat-card', accent && `stat-card--${accent}`)}>
      <p className="stat-label">{label}</p>
      <p className="stat-value" title={fullValue} aria-label={fullValue ?? String(value)}>
        {numericValue == null ? value : <Counter value={numericValue} formatted={value} currency={currency} />}
      </p>
      {sub && <p className="stat-sub">{sub}</p>}
    </div>
  );
});

// Animation state stays inside this leaf: neither the panel nor app rerenders
// on animation frames. The target is always a value returned by the server.
function Counter({ value, formatted, currency }: { value: number; formatted: string | number; currency?: string }) {
  const [shown, setShown] = useState(value);
  const current = useRef(value);
  useEffect(() => {
    const from = current.current;
    if (from === value) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const started = performance.now();
    let frame: number;
    const tick = (now: number) => {
      const progress = reduced ? 1 : Math.min((now - started) / 500, 1);
      const next = from + (value - from) * (1 - Math.pow(1 - progress, 3));
      current.current = next;
      setShown(next);
      if (progress < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [value]);
  return <span aria-hidden="true">{shown === value ? formatted : currency ? formatCompactCurrency(shown, currency) : formatCompactNumber(Math.round(shown))}</span>;
}

