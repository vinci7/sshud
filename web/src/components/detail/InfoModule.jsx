import { Accordion, AccordionItem } from '@carbon/react';
import { t } from '../../lib/i18n.js';
import { duration } from '../../lib/format.js';

// InfoModule: slow-changing system facts, folded by default.
export default function InfoModule({ s }) {
  const temps = s.temps?.length ? s.temps.map((x) => `${x.zone} ${x.celsius.toFixed(0)}°C`).join(', ') : '—';
  const updates = s.updates
    ? t('detail.updatesValue', { total: s.updates.total, security: s.updates.security })
    : t('detail.unknown');
  const items = [
    [t('detail.hostname'), s.host.hostname],
    [t('detail.os'), s.host.os],
    [t('detail.kernel'), s.host.kernel],
    [t('detail.arch'), s.host.arch],
    [t('detail.uptime'), duration(s.host.uptime)],
    [t('detail.addrs'), s.addrs?.join(', ') || '—'],
    [t('detail.updates'), updates],
    [t('detail.reboot'), s.reboot == null ? t('detail.unknown') : s.reboot ? t('detail.yes') : t('detail.no')],
    [t('detail.temps'), temps],
  ];
  return (
    <Accordion className="sb-info">
      <AccordionItem title={<span className="sb-module__title">{t('detail.sysinfo')}</span>}>
        <dl className="sb-dl">
          {items.map(([k, v]) => (
            <div key={k}>
              <dt>{k}</dt>
              <dd className="sb-mono">{v || '—'}</dd>
            </div>
          ))}
        </dl>
      </AccordionItem>
    </Accordion>
  );
}
