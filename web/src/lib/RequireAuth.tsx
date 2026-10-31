import { Navigate, useLocation } from 'react-router-dom';
import { auth } from '../lib/api';

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  if (!auth.isAuthenticated()) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }
  return <>{children}</>;
}

