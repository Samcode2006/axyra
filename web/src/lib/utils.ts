import { clsx } from 'clsx';

export function cn(...inputs: (string | undefined | null | boolean)[]) {
  return clsx(inputs);
}

export function formatCurrency(amount: number, currency = 'INR') {
  return new Intl.NumberFormat('en-IN', { style: 'currency', currency }).format(amount);
}

export function formatNumber(n: number) {
  return new Intl.NumberFormat('en-IN').format(n);
}

export function formatDate(iso: string) {
  return new Date(iso).toLocaleString('en-IN', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function relativeTime(iso: string) {
  const diff = Date.now() - new Date(iso).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function dateRangeParams(days = 7) {
  const to = new Date().toISOString();
  const from = new Date(Date.now() - days * 86_400_000).toISOString();
  return { from, to };
}

/**
 * A range of whole UTC days ending now: `days` = 1 is today since UTC midnight,
 * 7 is today plus the six days before it. The analytics API rounds every range
 * out to whole UTC days, so starting on a day boundary keeps hourly and daily
 * panels in agreement.
 */
export function utcDayRange(days: number, now: Date = new Date()) {
  const start = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()) - (days - 1) * 86_400_000;
  return { from: new Date(start).toISOString(), to: now.toISOString() };
}

