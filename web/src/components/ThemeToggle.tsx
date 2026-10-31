import { useState } from 'react';
import { Moon, Sun } from 'lucide-react';
import { initialTheme, saveTheme, type Theme } from '../lib/theme';

export function ThemeToggle({ className = '' }: { className?: string }) {
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const next = theme === 'dark' ? 'light' : 'dark';
  const Icon = next === 'dark' ? Moon : Sun;

  const toggle = () => {
    saveTheme(next);
    setTheme(next);
  };

  return (
    <button type="button" className={`btn theme-toggle ${className}`} onClick={toggle} aria-label={`Switch to ${next} mode`}>
      <Icon size={16} aria-hidden="true" />
      <span>{next === 'dark' ? 'Dark mode' : 'Light mode'}</span>
    </button>
  );
}
