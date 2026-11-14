import { lang, t } from './i18n.js';

// Docker's status column is English prose ("Up 7 weeks (healthy)",
// "Exited (137) 2 hours ago"). English shows it as is; other languages get
// it parsed and translated, falling back to the raw text when the format is
// not recognised.

const UNITS = ['second', 'minute', 'hour', 'day', 'week', 'month', 'year'];
const HEALTH = { healthy: 'healthy', unhealthy: 'unhealthy', 'health: starting': 'starting', paused: 'paused' };

// "7 weeks", "About an hour", "Less than a second" -> "7 周", "约 1 小时", "不到 1 秒"
function span(text) {
  const m = text.match(/^(?:(about|less than) )?(a|an|\d+) (second|minute|hour|day|week|month|year)s?$/i);
  if (!m || !UNITS.includes(m[3].toLowerCase())) return null;
  const n = /^\d+$/.test(m[2]) ? m[2] : '1';
  const s = t(`docker.unit.${m[3].toLowerCase()}`, { n });
  if (!m[1]) return s;
  return t(m[1].toLowerCase() === 'about' ? 'docker.about' : 'docker.under', { d: s });
}

function translate(status) {
  let m = status.match(/^Up (.+?)(?: \(([^)]+)\))?$/);
  if (m) {
    const d = span(m[1]);
    const h = m[2] && HEALTH[m[2].toLowerCase()];
    if (!d || (m[2] && !h)) return null;
    return t('docker.up', { d }) + (h ? t(`docker.health.${h}`) : '');
  }
  m = status.match(/^(Exited|Restarting) \((-?\d+)\) (.+) ago$/);
  if (m) {
    const ago = span(m[3]);
    return ago && t(`docker.${m[1].toLowerCase()}`, { code: m[2], ago });
  }
  const simple = { Created: 'created', Dead: 'dead', Paused: 'paused', 'Removal In Progress': 'removing' }[status];
  return simple ? t(`docker.${simple}`) : null;
}

export function containerStatus(status) {
  if (!status || lang === 'en') return status;
  return translate(status) ?? status;
}

// containerTone picks the status dot colour from docker's state: a clean
// exit (code 0, e.g. a nightly backup job) is neutral, not a failure.
export function containerTone(c) {
  switch (c.state) {
    case 'running':
      return 'ok';
    case 'restarting':
      return 'warn';
    case 'exited':
      return /^Exited \(0\)/.test(c.status) ? 'idle' : 'alert';
    case 'dead':
      return 'alert';
    default:
      return 'idle'; // created, paused, removing
  }
}
