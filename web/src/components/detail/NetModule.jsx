import { SkeletonText } from '@carbon/react';
import { LineChart } from '@carbon/charts-react';
import Module from './Module.jsx';
import { chartOptions, timeAxis } from './chart.js';
import { t } from '../../lib/i18n.js';
import { bytes, rate } from '../../lib/format.js';
import { useCarbonThemeName } from '../../lib/prefs.js';

// NetModule: upload / download chart, TCP retransmits and totals since boot.
export default function NetModule({ s, history }) {
  const theme = useCarbonThemeName();
  // null values (first frame, gaps) break the lines
  const data = history.flatMap((p) => [
    { group: t('detail.down'), date: new Date(p.time), value: p.rx ?? null },
    { group: t('detail.up'), date: new Date(p.time), value: p.tx ?? null },
  ]);
  const retrans = s.tcp?.retransPercent;
  return (
    <Module title={t('detail.network')} aside={retrans != null ? t('detail.retrans', { v: retrans.toFixed(2) }) : null}>
      {data.length > 2 ? (
        <LineChart
          data={data}
          options={chartOptions(theme, {
            axes: {
              bottom: timeAxis(data[0].date, data.at(-1).date),
              left: { mapsTo: 'value', ticks: { number: 3, formatter: (v) => rate(v) } },
            },
            curve: 'curveMonotoneX',
            legend: { enabled: true, alignment: 'left', clickable: false },
          })}
        />
      ) : (
        <SkeletonText paragraph lineCount={4} />
      )}
      <p className="sb-mono sb-muted">
        ↑ {rate(s.net?.txRate)} · ↓ {rate(s.net?.rxRate)} · {t('detail.sinceBoot')} ↑ {bytes(s.net?.txTotal)} ↓ {bytes(s.net?.rxTotal)}
      </p>
    </Module>
  );
}
