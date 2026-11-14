import { Breadcrumb, BreadcrumbItem, ContentSwitcher, Switch, Tag } from '@carbon/react';
import LiveStatus from '../LiveStatus.jsx';
import { t } from '../../lib/i18n.js';
import { bytes, duration, shortOS } from '../../lib/format.js';
import { onLinkClick } from '../../lib/router.js';

// DetailHeader: breadcrumb, name with status tag, a one-line host summary
// and the time range switch (only "live" works until history exists).
export default function DetailHeader({ machine, update, updates, lost }) {
  const s = update?.snapshot;
  const sub = s
    ? [shortOS(s.host.os), s.host.kernel && `kernel ${s.host.kernel}`, s.host.arch, `${s.host.cores}C`, bytes(s.mem?.total), s.addrs?.[0]?.split('/')[0]]
        .filter(Boolean)
        .join(' · ')
    : machine.host;
  return (
    <>
      <Breadcrumb noTrailingSlash className="sb-detail__crumb">
        <BreadcrumbItem href="/" onClick={onLinkClick}>
          {t('overview.title')}
        </BreadcrumbItem>
        <BreadcrumbItem isCurrentPage>{machine.name}</BreadcrumbItem>
      </Breadcrumb>
      <div className="sb-detail__head">
        <div>
          <h2 className="sb-detail__title">
            {machine.name}
            {update?.status === 'ok' && <Tag type="green" size="sm">{t('card.up', { d: duration(s?.host?.uptime) })}</Tag>}
            {update?.status === 'error' && <Tag type="red" size="sm">{t('card.offline')}</Tag>}
          </h2>
          <p className="sb-card__sub">{sub}</p>
        </div>
        <div className="sb-detail__range">
          <LiveStatus updates={updates} lost={lost} />
          <ContentSwitcher size="sm" selectedIndex={0} onChange={() => {}} className="sb-range">
            <Switch name="live" text={t('detail.live')} />
            <Switch name="1h" text="1h" disabled />
            <Switch name="24h" text="24h" disabled />
            <Switch name="7d" text="7d" disabled />
          </ContentSwitcher>
          <p className="sb-label">{t('detail.historyHint')}</p>
        </div>
      </div>
    </>
  );
}
