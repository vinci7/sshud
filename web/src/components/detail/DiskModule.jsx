import Module from './Module.jsx';
import Meter from '../Meter.jsx';
import SimpleTable from '../SimpleTable.jsx';
import { t } from '../../lib/i18n.js';
import { bytes, percent, rate } from '../../lib/format.js';
import { level } from '../../lib/thresholds.js';

// DiskModule: one row per mount point, IO rates in the header.
export default function DiskModule({ s }) {
  const io = s.diskio;
  const aside = io
    ? `${t('detail.read')} ${rate(io.readRate)} · ${t('detail.write')} ${rate(io.writeRate)} · ${Math.round(io.readIops + io.writeIops)} IOPS`
    : null;
  const rows = (s.disks ?? []).map((d) => ({
    id: d.path,
    path: <span className="sb-mono">{d.path}</span>,
    used: (
      <span className="sb-mono sb-nowrap">
        {bytes(d.used)} / {bytes(d.size)}
        {/* phones hide the usage column; the percentage moves here */}
        <span className="sb-phone-only" data-level={level('disk', d.percent)}>
          {' '}
          · {percent(d.percent)}%
        </span>
      </span>
    ),
    pct: (
      <span className="sb-pct">
        <Meter label={d.path} segments={[{ value: d.percent, kind: level('disk', d.percent) }]} />
        <span className="sb-mono">{percent(d.percent)}%</span>
      </span>
    ),
  }));
  return (
    <Module title={t('detail.disks')} aside={aside} className="sb-disks">
      <SimpleTable
        headers={[
          { key: 'path', header: t('detail.mount') },
          { key: 'used', header: t('detail.usedOfSize') },
          { key: 'pct', header: t('detail.usage') },
        ]}
        rows={rows}
        empty={t('detail.unsupported', { what: 'df' })}
      />
    </Module>
  );
}
