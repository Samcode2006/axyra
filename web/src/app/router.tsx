import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AppLayout } from '../components/AppLayout';
import { RequireAuth } from '../lib/RequireAuth';
import { RequireOperator } from '../lib/RequireOperator';
import LoginPage from '../features/auth/LoginPage';
import OverviewPage from '../features/overview/OverviewPage';
import TenantsPage from '../features/tenants/TenantsPage';
import ApplicationsPage from '../features/applications/ApplicationsPage';
import AnalyticsPage from '../features/analytics/AnalyticsPage';
import AccessPoliciesPage from '../features/access-policies/AccessPoliciesPage';
import MetricConfigPage from '../features/metric-config/MetricConfigPage';
import PlatformOperationsPage from '../features/platform-operations/PlatformOperationsPage';

// Monitoring arrives in Phase 5.
const DevAPlaceholder = ({ name }: { name: string }) => (
  <div className="page">
    <div className="page-header"><h1>{name}</h1></div>
    <div className="panel"><p style={{ opacity: 0.6 }}>Coming in a later phase.</p></div>
  </div>
);

export function AppRouter() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          element={
            <RequireAuth>
              <AppLayout />
            </RequireAuth>
          }
        >
          <Route index element={<OverviewPage />} />
          <Route path="tenants" element={<TenantsPage />} />
          <Route path="applications" element={<ApplicationsPage />} />
          <Route path="analytics" element={<AnalyticsPage />} />
          <Route path="access-policies" element={<AccessPoliciesPage />} />
          <Route path="metric-config" element={<MetricConfigPage />} />
          <Route
            path="platform-operations"
            element={
              <RequireOperator>
                <PlatformOperationsPage />
              </RequireOperator>
            }
          />
          <Route path="monitoring" element={<DevAPlaceholder name="Monitoring" />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

