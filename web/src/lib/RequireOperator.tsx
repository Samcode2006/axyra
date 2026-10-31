import { Navigate } from 'react-router-dom';
import { auth } from './api';

/** UI-only guard. The API independently refuses non-operators. */
export function RequireOperator({ children }: { children: React.ReactNode }) {
  if (!auth.isOperator()) return <Navigate to="/" replace />;
  return <>{children}</>;
}
