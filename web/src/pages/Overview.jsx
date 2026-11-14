import { useMemo, useState } from 'react';
import { Dropdown, InlineNotification } from '@carbon/react';
import MachineCard, { cardState } from '../components/MachineCard.jsx';
import LiveStatus from '../components/LiveStatus.jsx';
import StaleProbeNotice from '../components/StaleProbeNotice.jsx';
import { useStream } from '../lib/api.js';
import { t } from '../lib/i18n.js';
import { mainDisk, RANK } from '../lib/thresholds.js';

const SORTS = ['name', 'status', 'cpu', 'mem', 'disk'];

// severity: errors first, then the worst threshold level, then still connecting
function severity(u) {
  const { status, bar } = cardState(u);
  if (status === 'error') return 10;
  return RANK[bar] ?? -1;
}

const value = {
  cpu: (u) => u?.snapshot?.cpu?.percent ?? -1,
  mem: (u) => u?.snapshot?.mem?.percent ?? -1,
  disk: (u) => mainDisk(u?.snapshot?.disks)?.percent ?? -1,
};

export default function Overview({ machines }) {
  const { updates, lost } = useStream('level=overview');
  const [sort, setSort] = useState(() => localStorage.getItem('sort') || 'name');

  const sorted = useMemo(() => {
    const list = [...machines];
    const byName = (a, b) => a.name.localeCompare(b.name);
    if (sort === 'status') list.sort((a, b) => severity(updates[b.id]) - severity(updates[a.id]) || byName(a, b));
    else if (value[sort]) list.sort((a, b) => value[sort](updates[b.id]) - value[sort](updates[a.id]) || byName(a, b));
    else list.sort(byName);
    return list;
  }, [machines, updates, sort]);

  const up = machines.filter((m) => updates[m.id]?.status === 'ok').length;

  return (
    <section className="sb-overview">
      <div className="sb-overview__head">
        <h2 className="sb-overview__title">
          {t('overview.title')} <span className="sb-overview__count">{t('overview.online', { up, total: machines.length })}</span>
        </h2>
        <LiveStatus updates={updates} lost={lost} />
        <Dropdown
          id="sort"
          className="sb-overview__sort"
          type="inline"
          size="sm"
          titleText={t('overview.sort')}
          label={t('overview.sort')}
          items={SORTS}
          itemToString={(k) => (k ? t(`sort.${k}`) : '')}
          selectedItem={sort}
          onChange={({ selectedItem }) => {
            setSort(selectedItem);
            localStorage.setItem('sort', selectedItem);
          }}
        />
      </div>
      {lost && <InlineNotification kind="warning" lowContrast hideCloseButton title={t('stream.lost')} />}
      <StaleProbeNotice machines={machines} updates={updates} />
      <div className="sb-grid">
        {sorted.map((m) => (
          <MachineCard key={m.id} machine={m} update={updates[m.id]} />
        ))}
      </div>
    </section>
  );
}
