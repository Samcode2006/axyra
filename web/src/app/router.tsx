import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AppLayout } from '../components/AppLayout';
import { RequireAuth } from '../lib/RequireAuth';
import { RequireOperator } from '../lib/RequireOperator';
import LoginPage from '../features/auth/LoginPage';
import OverviewPage from '../features/overview/OverviewPage';
import TenantsPage from '../features/tenants/TenantsPage';
import ApplicationsPage from '../features/applications/ApplicationsPage';
import AnalyticsPage from '../features/analytics/AnalyticsPage';
import ComparisonPage from '../features/comparison/ComparisonPage';
import AccessPoliciesPage from '../features/access-policies/AccessPoliciesPage';
import MetricConfigPage from '../features/metric-config/MetricConfigPage';
import MonitoringPage from '../features/monitoring/MonitoringPage';
import PlatformOperationsPage from '../features/platform-operations/PlatformOperationsPage';

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
          <Route path="comparison" element={<ComparisonPage />} />
          <Route path="access-policies" element={<AccessPoliciesPage />} />
          <Route path="monitoring" element={<MonitoringPage />} />
          <Route path="metric-config" element={<MetricConfigPage />} />
          <Route
            path="platform-operations"
            element={
              <RequireOperator>
                <PlatformOperationsPage />
              </RequireOperator>
            }
          />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

