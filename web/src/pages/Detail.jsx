import { useEffect, useRef } from 'react';
import { ActionableNotification, InlineNotification, SkeletonText, Tile } from '@carbon/react';
import '@carbon/charts-react/styles.css';
import DetailHeader from '../components/detail/DetailHeader.jsx';
import OverviewModule from '../components/detail/OverviewModule.jsx';
import CpuModule from '../components/detail/CpuModule.jsx';
import MemModule from '../components/detail/MemModule.jsx';
import NetModule from '../components/detail/NetModule.jsx';
import DiskModule from '../components/detail/DiskModule.jsx';
import ProcModule from '../components/detail/ProcModule.jsx';
import DockerModule from '../components/detail/DockerModule.jsx';
import InfoModule from '../components/detail/InfoModule.jsx';
import HostKeyNotice from '../components/HostKeyNotice.jsx';
import { useStream } from '../lib/api.js';
import { t } from '../lib/i18n.js';
import { navigate } from '../lib/router.js';

const HISTORY = 150; // points kept for the live charts (5 min at 2 s)
const GAP = 15000; // ms without a frame (nobody was watching) that breaks the line

// useHistory starts from the server's last 5 minutes and appends each new
// snapshot to a bounded series; a null point marks each gap.
function useHistory(u) {
  const ref = useRef([]);
  const s = u?.snapshot;
  const now = s ? [{ time: s.time, cpu: s.cpu?.percent, rx: s.net?.rxRate, tx: s.net?.txRate }] : [];
  for (const p of [...(u?.history ?? []), ...now]) {
    const last = ref.current.at(-1);
    if (last && p.time <= last.time) continue;
    const gap = last && p.time - last.time > GAP ? [{ time: last.time + 1, cpu: null, rx: null, tx: null }] : [];
    ref.current = [...ref.current.slice(-(HISTORY - 2)), ...gap, p];
  }
  return ref.current;
}

// Detail is one machine: eight modules fed by a 2-second detail stream.
export default function Detail({ machine, readOnly }) {
  const { updates, lost } = useStream(`level=detail&id=${encodeURIComponent(machine.id)}`);
  const u = updates[machine.id];
  const s = u?.snapshot;
  const history = useHistory(u);
  const kind = u?.error?.kind ?? 'internal';

  useEffect(() => {
    document.title = `${machine.name} · sshud`;
    return () => {
      document.title = 'sshud';
    };
  }, [machine.name]);

  return (
    <section className="sb-detail">
      <DetailHeader machine={machine} update={u} updates={updates} lost={lost} />

      {u?.status === 'error' && kind === 'hostkey' && <HostKeyNotice machine={machine} message={u.error.message} readOnly={readOnly} />}
      {u?.status === 'error' && kind !== 'hostkey' && (
        <InlineNotification
          kind="error"
          lowContrast
          hideCloseButton
          title={t(`error.${kind}`)}
          subtitle={`${t(`hint.${kind}`)} (${u.error?.message})`}
        />
      )}

      {s?.probeOutdated && (
        <ActionableNotification
          kind="warning"
          inline
          lowContrast
          hideCloseButton
          title={t('outdated.title')}
          subtitle={t('outdated.body')}
          actionButtonLabel={t('outdated.action')}
          onActionButtonClick={() => navigate('/machines')}
        />
      )}

      {!s ? (
        <div className="sb-detail__grid">
          {[0, 1, 2].map((i) => (
            <Tile key={i} className="sb-module">
              <SkeletonText paragraph lineCount={6} />
            </Tile>
          ))}
        </div>
      ) : (
        <>
          <div className="sb-detail__grid sb-detail__grid--3">
            <OverviewModule s={s} />
            <CpuModule s={s} history={history} />
            <MemModule s={s} />
          </div>
          <div className="sb-detail__grid sb-detail__grid--2">
            <NetModule s={s} history={history} />
            <DiskModule s={s} />
            <ProcModule s={s} />
            <DockerModule s={s} />
          </div>
          <InfoModule s={s} />
        </>
      )}
    </section>
  );
}
