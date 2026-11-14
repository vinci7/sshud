import { SkeletonText } from '@carbon/react';
import { AreaChart } from '@carbon/charts-react';
import Module from './Module.jsx';
import { chartOptions, timeAxis } from './chart.js';
import { t } from '../../lib/i18n.js';
import { percent } from '../../lib/format.js';
import { level } from '../../lib/thresholds.js';
import { useCarbonThemeName } from '../../lib/prefs.js';

const MAX_CORES = 64;

// CpuModule: live usage chart, user/sys/iowait split and one cell per core.
export default function CpuModule({ s, history }) {
  const theme = useCarbonThemeName();
  const c = s.cpu;
  // null values (gaps) break the line instead of joining across them
  const data = history.map((p) => ({ group: 'CPU', date: new Date(p.time), value: p.cpu ?? null }));
  const cores = c?.cores ?? [];
  const aside = c
    ? `user ${percent(c.user)} · sys ${percent(c.system)} · iowait ${percent(c.iowait)}${c.steal >= 0.5 ? ` · steal ${percent(c.steal)}` : ''}`
    : null;
  return (
    <Module title="CPU" aside={aside}>
      {data.length > 1 ? (
        <AreaChart
          data={data}
          options={chartOptions(theme, {
            axes: {
              bottom: timeAxis(data[0].date, data.at(-1).date),
              left: { mapsTo: 'value', domain: [0, 100], ticks: { values: [0, 50, 100] } },
            },
            color: { scale: { CPU: '#4589ff' } },
          })}
        />
      ) : (
        <SkeletonText paragraph lineCount={4} />
      )}
      {cores.length > 0 && (
        <>
          <p className="sb-label">{t('detail.perCore', { n: cores.length })}</p>
          <div className="sb-cores">
            {cores.slice(0, MAX_CORES).map((v, i) => (
              <span key={i} className="sb-core" data-level={level('cpu', v)} title={`cpu${i} ${percent(v)}%`}>
                <span className="sb-core__fill" style={{ height: `${Math.max(2, v)}%` }} />
              </span>
            ))}
          </div>
        </>
      )}
    </Module>
  );
}
