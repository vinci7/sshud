import { useEffect, useState } from 'react';
import { CircleFilled } from '@carbon/icons-react';
import { t } from '../lib/i18n.js';
import { ago } from '../lib/format.js';

// Frames arrive every 2–5 s; only when the newest is older than this does the
// age matter enough to show (e.g. after the tab sat in the background).
const STALE_MS = 15000;

// LiveStatus shows that numbers are flowing: a green dot and "Live". The age
// of the newest frame appears only once it falls behind.
export default function LiveStatus({ updates, lost }) {
  const [, tick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, []);
  const newest = Math.max(0, ...Object.values(updates).map((u) => u.snapshot?.time ?? 0));
  const stale = newest > 0 && Date.now() - newest > STALE_MS;
  return (
    <span className="sb-live" data-live={!lost && newest > 0 && !stale}>
      <CircleFilled size={12} />
      {lost ? t('live.paused') : t('live.on')}
      {stale && <span className="sb-live__age">· {t('live.updated', { ago: ago(newest) })}</span>}
    </span>
  );
}
