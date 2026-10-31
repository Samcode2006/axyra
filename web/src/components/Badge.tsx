interface BadgeProps {
  status: 'active' | 'suspended' | 'inactive' | 'connected' | 'disconnected' | 'error' | string;
}

const COLOR: Record<string, string> = {
  active: 'badge--green',
  connected: 'badge--green',
  suspended: 'badge--amber',
  inactive: 'badge--gray',
  disconnected: 'badge--gray',
  error: 'badge--red',
};

export function Badge({ status }: BadgeProps) {
  return (
    <span className={`badge ${COLOR[status] ?? 'badge--gray'}`}>
      {status}
    </span>
  );
}

