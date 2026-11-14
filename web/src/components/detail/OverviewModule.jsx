import Module from './Module.jsx';
import Ring from '../Ring.jsx';
import { t } from '../../lib/i18n.js';
import { duration } from '../../lib/format.js';
import { level, mainDisk } from '../../lib/thresholds.js';

// OverviewModule: the three rings plus load, processes, users and uptime.
export default function OverviewModule({ s }) {
  const disk = mainDisk(s.disks);
  const stats = [
    [t('card.load'), s.load ? [s.load.load1, s.load.load5, s.load.load15].map((x) => x.toFixed(2)).join(' ') : '—'],
    [t('detail.procs'), s.load?.procs ?? '—'],
    [t('detail.users'), s.users ? s.users.length : '—'],
    [t('detail.uptime'), duration(s.host.uptime)],
  ];
  return (
    <Module title={t('detail.overview')}>
      <div className="sb-card__rings">
        <Ring label={t('metric.cpu')} value={s.cpu?.percent} level={level('cpu', s.cpu?.percent)} />
        <Ring label={t('metric.mem')} value={s.mem?.percent} level={level('mem', s.mem?.percent)} />
        <Ring label={t('metric.disk')} value={disk?.percent} level={level('disk', disk?.percent)} />
      </div>
      <dl className="sb-stats">
        {stats.map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </Module>
  );
}
