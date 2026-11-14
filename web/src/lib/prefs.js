import { createContext, useContext, useEffect, useState } from 'react';

// CarbonTheme carries "g100" or "g10" to components that theme themselves,
// such as Carbon Charts.
export const CarbonTheme = createContext('g100');
export const useCarbonThemeName = () => useContext(CarbonTheme);

// Display preferences live in localStorage; "system" follows the OS.
export const THEMES = ['system', 'dark', 'light'];
export const LANGS = ['auto', 'en', 'zh'];

export function themeChoice() {
  const v = localStorage.getItem('theme');
  return THEMES.includes(v) ? v : 'system';
}

export function langChoice() {
  const v = localStorage.getItem('lang');
  return LANGS.includes(v) ? v : 'auto';
}

const dark = () => window.matchMedia('(prefers-color-scheme: dark)');

// useCarbonTheme returns "g100" or "g10" and follows OS changes when the
// choice is "system". Dark is the default when the OS has no preference.
export function useCarbonTheme(choice) {
  const [osDark, setOsDark] = useState(() => !window.matchMedia('(prefers-color-scheme: light)').matches);
  useEffect(() => {
    const mq = dark();
    const on = () => setOsDark(mq.matches || !window.matchMedia('(prefers-color-scheme: light)').matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  const isDark = choice === 'dark' || (choice === 'system' && osDark);
  // The theme class also goes on <html> so overscroll / rubber-band areas
  // outside the app show the theme background instead of white.
  useEffect(() => {
    const root = document.documentElement;
    root.style.colorScheme = isDark ? 'dark' : 'light';
    root.classList.toggle('cds--g100', isDark);
    root.classList.toggle('cds--g10', !isDark);
  }, [isDark]);
  return isDark ? 'g100' : 'g10';
}
