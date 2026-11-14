import Module, { Unsupported } from './Module.jsx';
import Meter from '../Meter.jsx';
import { t } from '../../lib/i18n.js';
import { bytes } from '../../lib/format.js';
import { level } from '../../lib/thresholds.js';

// MemModule: used / cache / free stacked bar, then swap.
export default function MemModule({ s }) {
  const m = s.mem;
  if (!m) {
    return (
      <Module title={t('detail.memory')}>
        <Unsupported what="/proc/meminfo" />
      </Module>
    );
  }
  const pct = (x) => (x * 100) / m.total;
  const swapPct = m.swapTotal ? (m.swapUsed * 100) / m.swapTotal : 0;
  return (
    <Module title={t('detail.memory')} aside={`${bytes(m.used)} / ${bytes(m.total)}`}>
      <Meter
        label={t('detail.memory')}
        segments={[
          { value: pct(m.used), kind: level('mem', m.percent) },
          { value: pct(m.cache), kind: 'cache' },
        ]}
      />
      <p className="sb-legend">
        <span data-kind={level('mem', m.percent)}>{t('detail.memApps')} {bytes(m.used)}</span>
        <span data-kind="cache">{t('detail.memCache')} {bytes(m.cache)}</span>
        <span data-kind="free">{t('detail.memFree')} {bytes(m.free)}</span>
      </p>
      <p className="sb-label">Swap</p>
      {m.swapTotal ? (
        <>
          <Meter label="Swap" segments={[{ value: swapPct, kind: level('mem', swapPct) }]} />
          <p className="sb-mono sb-muted">
            {bytes(m.swapUsed)} / {bytes(m.swapTotal)}
          </p>
        </>
      ) : (
        <p className="sb-muted">{t('detail.noSwap')}</p>
      )}
    </Module>
  );
}
