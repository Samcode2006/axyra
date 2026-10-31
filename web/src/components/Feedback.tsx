import { Loader2 } from 'lucide-react';

export function Spinner({ size = 20 }: { size?: number }) {
  return (
    <div className="spinner-wrap">
      <Loader2 size={size} className="spinner" />
    </div>
  );
}

export function ErrorMsg({ message }: { message: string }) {
  return <div className="error-msg">⚠ {message}</div>;
}

export function EmptyState({ message }: { message: string }) {
  return <div className="empty-state">{message}</div>;
}

