import { t } from './i18n.js';

const UNITS = ['B', 'K', 'M', 'G', 'T', 'P'];

// Compact binary size: 1536 -> "1.5K", 2.1e9 -> "2.0G".
export function bytes(n) {
  if (n == null || Number.isNaN(n)) return '—';
  let i = 0;
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)}${UNITS[i]}`;
}

export function rate(n) {
  return n == null ? '—' : `${bytes(n)}/s`;
}

export function percent(n) {
  return n == null ? '—' : String(Math.round(n));
}

// Uptime in its largest unit: 41d, 5h, 12m.
export function duration(sec) {
  if (sec == null) return '—';
  if (sec >= 86400) return `${Math.floor(sec / 86400)}d`;
  if (sec >= 3600) return `${Math.floor(sec / 3600)}h`;
  return `${Math.max(1, Math.floor(sec / 60))}m`;
}

export function ago(ms, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 3) return t('ago.now');
  if (s < 60) return t('ago.s', { n: s });
  if (s < 3600) return t('ago.m', { n: Math.floor(s / 60) });
  if (s < 86400) return t('ago.h', { n: Math.floor(s / 3600) });
  return t('ago.d', { n: Math.floor(s / 86400) });
}

// "Ubuntu 24.04.4 LTS" -> "Ubuntu 24.04", "Raspbian GNU/Linux 12 (bookworm)" -> "Raspbian 12".
export function shortOS(pretty) {
  if (!pretty) return '';
  return pretty
    .replace(/GNU\/Linux\s*/i, '')
    .replace(/\s*\(.*\)\s*/, '')
    .replace(/\s+LTS$/i, '')
    .replace(/^(\S+ \d+\.\d+)\.\d+/, '$1')
    .replace(/\s+v(\d)/, ' $1')
    .trim();
}
