import { useState } from 'react';
import { ContentSwitcher, Switch } from '@carbon/react';
import Module from './Module.jsx';
import SimpleTable from '../SimpleTable.jsx';
import { t } from '../../lib/i18n.js';
import { bytes } from '../../lib/format.js';

// ProcModule: the top five processes, sorted by CPU or memory.
export default function ProcModule({ s }) {
  const [by, setBy] = useState('cpu');
  const procs = [...(s.procs ?? [])]
    .sort((a, b) => (by === 'cpu' ? (b.cpu ?? -1) - (a.cpu ?? -1) || b.rss - a.rss : b.rss - a.rss))
    .slice(0, 5);
  const rows = procs.map((p) => ({
    id: String(p.pid),
    name: <span className="sb-mono" title={`pid ${p.pid}`}>{p.name}</span>,
    pid: <span className="sb-mono sb-muted">{p.pid}</span>,
    cpu: <span className="sb-mono">{p.cpu == null ? '—' : `${p.cpu.toFixed(1)}%`}</span>,
    mem: <span className="sb-mono">{bytes(p.rss)}</span>,
  }));
  return (
    <Module
      title={t('detail.topProcs')}
      aside={
        <ContentSwitcher size="sm" selectedIndex={by === 'cpu' ? 0 : 1} onChange={({ name }) => setBy(name)} className="sb-switch">
          <Switch name="cpu" text="CPU" />
          <Switch name="mem" text={t('detail.memShort')} />
        </ContentSwitcher>
      }
    >
      <SimpleTable
        headers={[
          { key: 'name', header: t('detail.process') },
          { key: 'pid', header: 'PID' },
          { key: 'cpu', header: 'CPU' },
          { key: 'mem', header: t('sort.mem') },
        ]}
        rows={rows}
        empty={t('detail.unsupported', { what: '/proc/*/stat' })}
      />
    </Module>
  );
}
