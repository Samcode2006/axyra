import { cn } from '../lib/utils';

interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  accent?: 'green' | 'blue' | 'amber' | 'red';
}

export function StatCard({ label, value, sub, accent }: StatCardProps) {
  return (
    <div className={cn('stat-card', accent && `stat-card--${accent}`)}>
      <p className="stat-label">{label}</p>
      <p className="stat-value">{value}</p>
      {sub && <p className="stat-sub">{sub}</p>}
    </div>
  );
}

