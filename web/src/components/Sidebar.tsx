import { NavLink } from 'react-router-dom';
import {
  BarChart2,
  GitCompareArrows,
  Building2,
  ChevronRight,
  Cpu,
  Globe,
  LayoutDashboard,
  LogOut,
  Server,
  Settings,
  ShieldCheck,
} from 'lucide-react';
import { auth } from '../lib/api';
import { cn } from '../lib/utils';
import { ThemeToggle } from './ThemeToggle';

interface NavEntry {
  to: string;
  icon: typeof Globe;
  label: string;
  end?: boolean;
  operatorOnly?: boolean;
}

const NAV: NavEntry[] = [
  { to: '/', icon: LayoutDashboard, label: 'Overview', end: true },
  { to: '/tenants', icon: Building2, label: 'Tenants' },
  { to: '/applications', icon: Cpu, label: 'Applications' },
  { to: '/analytics', icon: BarChart2, label: 'Analytics' },
  { to: '/comparison', icon: GitCompareArrows, label: 'Live Comparison' },
  { to: '/access-policies', icon: ShieldCheck, label: 'Access & Policies' },
  // Dev A owns these pages but they appear in the shared nav
  { to: '/monitoring', icon: Globe, label: 'Monitoring' },
  { to: '/metric-config', icon: Settings, label: 'Metric Config' },
  // Hidden from customer admins; the API refuses them regardless.
  { to: '/platform-operations', icon: Server, label: 'Platform Operations', operatorOnly: true },
];

export function Sidebar() {
  const user = auth.user();
  const entries = NAV.filter((n) => !n.operatorOnly || user?.role === 'operator');

  const signOut = () => {
    auth.logout();
    window.location.href = '/login';
  };

  return (
    <aside className="sidebar">
      <div className="sidebar-brand">
        <span className="brand-logo">Ax</span>
        <span className="brand-name">Axyra</span>
      </div>
      <nav className="sidebar-nav">
        {entries.map(({ to, icon: Icon, label, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) => cn('nav-item', isActive && 'nav-item--active')}
          >
            <Icon size={18} />
            <span>{label}</span>
            <ChevronRight size={14} className="nav-chevron" />
          </NavLink>
        ))}
      </nav>
      <div className="sidebar-footer">
        <ThemeToggle />
        {user && (
          <div className="sidebar-user">
            <span className="sidebar-user-name">{user.username}</span>
            <span className="sidebar-user-role">
              {user.role === 'operator' ? 'Platform operator' : user.organization_id}
            </span>
            <button className="btn btn--sm" onClick={signOut}>
              <LogOut size={12} /> Sign out
            </button>
          </div>
        )}
        <span className="version">v0.3.0-phase5</span>
      </div>
    </aside>
  );
}

