import type { ReactNode } from 'react';
import { Spinner } from '../../../components/Feedback';

interface PanelProps {
  title: ReactNode;
  loading: boolean;
  error: string | null;
  onRetry?: () => void;
  actions?: ReactNode;
  children: ReactNode;
}

/** A titled panel that shows a spinner, an inline error with retry, or its content. */
export function Panel({ title, loading, error, onRetry, actions, children }: PanelProps) {
  return (
    <section className="panel">
      <div className="panel-head">
        <h2 className="panel-title">{title}</h2>
        {actions}
      </div>
      {error ? (
        <div className="error-msg">
          {error}{' '}
          {onRetry && (
            <button className="btn btn--sm" onClick={onRetry}>
              Retry
            </button>
          )}
        </div>
      ) : loading ? (
        <Spinner />
      ) : (
        children
      )}
    </section>
  );
}

export function Empty({ children = 'No data for this period.' }: { children?: ReactNode }) {
  return <p className="empty-state" style={{ padding: '20px 0' }}>{children}</p>;
}

/** A horizontal bar whose width is value / max. */
export function Bar({ value, max, color = 'var(--accent)' }: { value: number; max: number; color?: string }) {
  const pct = max > 0 ? Math.max(Math.min((value / max) * 100, 100), value > 0 ? 2 : 0) : 0;
  return (
    <div className="hbar-track">
      <div className="hbar-fill" style={{ width: `${pct}%`, background: color }} />
    </div>
  );
}
