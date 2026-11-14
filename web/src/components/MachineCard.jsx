import { Button, SkeletonPlaceholder, Tag, Tile, Toggletip, ToggletipButton, ToggletipContent, Tooltip } from '@carbon/react';
import { Renew, WarningAltFilled } from '@carbon/icons-react';
import Ring from './Ring.jsx';
import { t } from '../lib/i18n.js';
import { ago, bytes, duration, rate, shortOS } from '../lib/format.js';
import { level, mainDisk, worst } from '../lib/thresholds.js';
import { retry } from '../lib/api.js';
import { machinePath, navigate, onLinkClick } from '../lib/router.js';

// cardState reduces an update to what the card shows and its top bar colour.
// The bar only reports thresholds; an unreachable machine gets a grey bar
// (the red "offline" tag already says so).
export function cardState(u) {
  const s = u?.snapshot;
  const status = u?.status ?? 'connecting';
  const disk = mainDisk(s?.disks);
  const levels = {
    cpu: level('cpu', s?.cpu?.percent),
    mem: level('mem', s?.mem?.percent),
    disk: level('disk', disk?.percent),
  };
  let bar = worst(Object.values(levels));
  if (status === 'error') bar = 'off';
  if (status === 'connecting' && !s) bar = 'none';
  return { status, s, disk, levels, bar };
}

export default function MachineCard({ machine, update }) {
  const { status, s, disk, levels, bar } = cardState(update);
  const partial = status === 'ok' && (s?.failed?.length > 0 || s?.probeOutdated);
  const kind = update?.error?.kind ?? 'internal';
  const sub = [shortOS(s?.host?.os), s?.host?.arch, s && `${s.host.cores}C/${bytes(s.mem?.total)}`]
    .filter(Boolean)
    .join(' · ');

  return (
    <Tile
      className="sb-card"
      data-bar={bar}
      onClick={(e) => {
        // the whole card opens the detail page, except its own controls
        if (!e.target.closest('a, button, [role="dialog"]')) navigate(machinePath(machine.id));
      }}
    >
      <div className="sb-card__head">
        <h3 className="sb-card__name" title={machine.name}>
          <a href={machinePath(machine.id)} onClick={onLinkClick}>
            {machine.name}
          </a>
        </h3>
        {status === 'ok' && <Tag type="green" size="sm">{t('card.up', { d: duration(s?.host?.uptime) })}</Tag>}
        {status === 'error' && <Tag type="red" size="sm">{t('card.offline')}</Tag>}
        {status === 'connecting' && <Tag type="gray" size="sm">{t('card.connecting')}</Tag>}
      </div>
      <p className="sb-card__sub">{sub || machine.host}</p>

      {status === 'error' && !s ? (
        <div className="sb-card__error">
          <p className="sb-card__errtitle">{t(`error.${kind}`)}</p>
          <p className="sb-card__muted">{t(`hint.${kind}`)}</p>
        </div>
      ) : !s ? (
        <div className="sb-card__rings">
          {[0, 1, 2].map((i) => (
            <SkeletonPlaceholder key={i} className="sb-ring-skeleton" />
          ))}
        </div>
      ) : (
        <div className="sb-card__rings">
          <Ring label={t('metric.cpu')} value={s.cpu?.percent} level={levels.cpu} />
          <Ring label={t('metric.mem')} value={s.mem?.percent} level={levels.mem} />
          {/* the ring shows the fullest mount; name it when there is more than one */}
          <Ring
            label={s.disks?.length > 1 ? `${t('metric.disk')} · ${disk.path}` : t('metric.disk')}
            value={disk?.percent}
            level={levels.disk}
          />
        </div>
      )}

      <div className="sb-card__foot">
        {status === 'error' ? (
          <>
            <span>
              {update.lastOk ? t('card.lastOk', { ago: ago(update.lastOk) }) : t('card.neverOk')}
              <Toggletip align="top-left">
                <ToggletipButton label={t('card.details')}>
                  <span className="sb-card__details">{t('card.details')}</span>
                </ToggletipButton>
                <ToggletipContent>
                  <code className="sb-card__errmsg">{update.error?.message}</code>
                </ToggletipContent>
              </Toggletip>
            </span>
            <Button kind="ghost" size="sm" renderIcon={Renew} onClick={() => retry(machine.id)}>
              {t('card.retry')}
            </Button>
          </>
        ) : (
          <>
            <span>
              {t('card.load')} {s?.load ? [s.load.load1, s.load.load5, s.load.load15].map((x) => x.toFixed(2)).join(' ') : '—'}
              {partial && (
                <Tooltip
                  align="top"
                  label={s.probeOutdated ? t('outdated.short') : t('card.partialHint', { list: s.failed.join(', ') })}
                >
                  <button type="button" className="sb-card__partial" aria-label={t('card.partial')}>
                    <WarningAltFilled />
                  </button>
                </Tooltip>
              )}
            </span>
            <span>
              ↑{rate(s?.net?.txRate)} ↓{rate(s?.net?.rxRate)}
            </span>
          </>
        )}
      </div>
    </Tile>
  );
}
