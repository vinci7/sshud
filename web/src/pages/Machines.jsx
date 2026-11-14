import { useEffect, useState } from 'react';
import { Button, InlineNotification, Modal, Tile } from '@carbon/react';
import { Add, DocumentImport } from '@carbon/icons-react';
import MachineList from '../components/machines/MachineList.jsx';
import MachineForm, { EMPTY_MACHINE } from '../components/machines/MachineForm.jsx';
import ImportDialog from '../components/machines/ImportDialog.jsx';
import SetupDialog from '../components/machines/SetupDialog.jsx';
import HostKeyNotice from '../components/HostKeyNotice.jsx';
import { api, useStream } from '../lib/api.js';
import { t } from '../lib/i18n.js';

// Machines is the management page: list, add / edit, connect, delete and
// import.
export default function Machines({ meta, onChanged }) {
  const [list, setList] = useState(null);
  const [error, setError] = useState(null);
  const [editing, setEditing] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [importing, setImporting] = useState(false);
  const [setup, setSetup] = useState(null);
  const { updates } = useStream('level=overview'); // to offer re-trusting changed host keys
  const readOnly = meta?.readOnly;

  const load = () =>
    api('GET', readOnly ? '/api/machines' : '/api/admin/machines')
      .then(setList)
      .catch((e) => setError(e.message));
  useEffect(() => {
    if (meta) load();
  }, [meta]); // eslint-disable-line react-hooks/exhaustive-deps

  const changed = () => {
    setEditing(null);
    setDeleting(null);
    load();
    onChanged();
  };
  // manual: the user installs the line by hand instead of bootstrapping
  const saved = (m, manual) => {
    if (manual) setSetup(m);
    changed();
  };

  return (
    <section>
      <div className="sb-overview__head">
        <h2 className="sb-overview__title">{t('manage.title')}</h2>
      </div>
      {readOnly && <InlineNotification kind="info" lowContrast hideCloseButton title={t('manage.readOnly')} />}
      {error && <InlineNotification kind="error" lowContrast hideCloseButton title={error} />}
      {list?.map(
        (m) =>
          updates[m.id]?.error?.kind === 'hostkey' && (
            <HostKeyNotice key={m.id} machine={m} message={`${m.name}: ${updates[m.id].error.message}`} readOnly={readOnly} />
          ),
      )}
      {list && list.length === 0 ? (
        <Tile className="sb-empty">
          <h3>{t('manage.emptyTitle')}</h3>
          <p className="sb-muted">{t('manage.emptyBody')}</p>
          <div className="sb-actions">
            <Button renderIcon={Add} onClick={() => setEditing(EMPTY_MACHINE)}>
              {t('manage.add')}
            </Button>
            <Button kind="tertiary" renderIcon={DocumentImport} onClick={() => setImporting(true)}>
              {t('import.open')}
            </Button>
          </div>
        </Tile>
      ) : (
        <MachineList
          list={list ?? []}
          readOnly={readOnly}
          onAdd={() => setEditing(EMPTY_MACHINE)}
          onImport={() => setImporting(true)}
          onEdit={setEditing}
          onSetup={setSetup}
          onDelete={setDeleting}
        />
      )}
      {importing && (
        <ImportDialog
          onClose={() => setImporting(false)}
          onImported={() => {
            load();
            onChanged();
          }}
        />
      )}
      {editing && <MachineForm initial={editing} onClose={() => setEditing(null)} onSaved={saved} />}
      {setup && <SetupDialog machine={setup} onClose={() => setSetup(null)} />}
      {deleting && (
        <Modal
          open
          danger
          size="xs"
          modalHeading={t('manage.deleteTitle', { name: deleting.name })}
          primaryButtonText={t('manage.delete')}
          secondaryButtonText={t('manage.cancel')}
          onRequestClose={() => setDeleting(null)}
          onRequestSubmit={() => api('DELETE', `/api/admin/machines/${encodeURIComponent(deleting.id)}`).then(changed, (e) => setError(e.message))}
        >
          <p>{t('manage.deleteBody')}</p>
        </Modal>
      )}
    </section>
  );
}
