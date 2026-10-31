import { NavLink } from 'react-router-dom';
import {
  BarChart2,
  Building2,
  ChevronRight,
  Cpu,
  Globe,
  LayoutDashboard,
  Settings,
  ShieldCheck,
} from 'lucide-react';
import { cn } from '../lib/utils';

const NAV = [
  { to: '/', icon: LayoutDashboard, label: 'Overview', end: true },
  { to: '/tenants', icon: Building2, label: 'Tenants' },
  { to: '/applications', icon: Cpu, label: 'Applications' },
  { to: '/analytics', icon: BarChart2, label: 'Analytics' },
  { to: '/access-policies', icon: ShieldCheck, label: 'Access & Policies' },
  // Dev A owns these pages but they appear in the shared nav
  { to: '/monitoring', icon: Globe, label: 'Monitoring' },
  { to: '/metric-config', icon: Settings, label: 'Metric Config' },
];

export function Sidebar() {
  return (
    <aside className="sidebar">
      <div className="sidebar-brand">
        <span className="brand-logo">Ax</span>
        <span className="brand-name">Axyra</span>
      </div>
      <nav className="sidebar-nav">
        {NAV.map(({ to, icon: Icon, label, end }) => (
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
        <span className="version">v0.1.0-alpha</span>
      </div>
    </aside>
  );
}

