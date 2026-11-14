import { useState } from 'react';
import { Button } from '@carbon/react';
import Module from './Module.jsx';
import SimpleTable from '../SimpleTable.jsx';
import { t } from '../../lib/i18n.js';
import { bytes } from '../../lib/format.js';
import { containerStatus, containerTone } from '../../lib/docker.js';

const ROWS = 8;

// DockerModule: stopped containers first, then by name; long lists fold.
export default function DockerModule({ s }) {
  const [all, setAll] = useState(false);
  const cs = s.containers;
  const failed = s.failed?.includes('docker');
  const running = cs?.filter((c) => c.running).length ?? 0;
  const aside = cs ? t('detail.containersCount', { up: running, down: cs.length - running }) : null;
  const sorted = [...(cs ?? [])].sort(
    (a, b) => Number(a.running) - Number(b.running) || a.name.localeCompare(b.name, undefined, { numeric: true }),
  );
  const rows = (all ? sorted : sorted.slice(0, ROWS)).map((c) => ({
    id: c.name,
    name: <span className="sb-mono">{c.name}</span>,
    state: (
      <span className="sb-state" data-level={containerTone(c)} title={c.status}>
        {containerStatus(c.status)}
      </span>
    ),
    image: <span className="sb-mono sb-muted">{c.image}</span>,
    cpu: <span className="sb-mono">{c.cpu == null ? '—' : `${c.cpu.toFixed(1)}%`}</span>,
    mem: <span className="sb-mono">{c.mem == null ? '—' : bytes(c.mem)}</span>,
  }));
  return (
    <Module title={t('detail.docker')} aside={aside} className="sb-docker">
      {failed && !cs ? (
        <p className="sb-muted">{t('detail.noDocker')}</p>
      ) : (
        <SimpleTable
          headers={[
            { key: 'name', header: t('detail.container') },
            { key: 'state', header: t('detail.state') },
            { key: 'cpu', header: 'CPU' },
            { key: 'mem', header: t('detail.memShort') },
            { key: 'image', header: t('detail.image') },
          ]}
          rows={rows}
          empty={t('detail.noContainers')}
        />
      )}
      {sorted.length > ROWS && (
        <Button kind="ghost" size="sm" className="sb-more" onClick={() => setAll(!all)}>
          {all ? t('detail.showLess') : t('detail.showAll', { n: sorted.length })}
        </Button>
      )}
    </Module>
  );
}
