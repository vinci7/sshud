import { useState } from 'react';
import { ActionableNotification, InlineNotification, Modal, Stack } from '@carbon/react';
import { api } from '../lib/api.js';
import { t } from '../lib/i18n.js';

// HostKeyNotice is the "host key changed" error with a Re-trust action: a
// modal shows the recorded and the presented fingerprint side by side, and
// only on confirmation does the server record the new key.
export default function HostKeyNotice({ machine, message, readOnly }) {
  const [keys, setKeys] = useState(null); // null: closed; {}: loading
  const trust = (fingerprint) =>
    api('POST', `/api/admin/machines/${encodeURIComponent(machine.id)}/trust`, { fingerprint }).then(
      (k) => setKeys(k.trusted ? null : k),
      (e) => setKeys({ error: e.message }),
    );
  return (
    <>
      <ActionableNotification
        kind="error"
        inline
        lowContrast
        hideCloseButton
        title={t('error.hostkey')}
        subtitle={`${t('hint.hostkey')} (${message})`}
        actionButtonLabel={readOnly ? undefined : t('retrust.open')}
        onActionButtonClick={() => {
          setKeys({});
          trust('');
        }}
      />
      {keys && (
        <Modal
          open
          danger
          modalHeading={t('retrust.title', { name: machine.name })}
          primaryButtonText={t('retrust.open')}
          primaryButtonDisabled={!keys.presented}
          secondaryButtonText={t('manage.cancel')}
          onRequestSubmit={() => trust(keys.presented)}
          onRequestClose={() => setKeys(null)}
        >
          <Stack gap={5}>
            <p>{t('retrust.body')}</p>
            <dl className="sb-dl">
              {['recorded', 'presented'].map((k) => (
                <div key={k}>
                  <dt>{t(`retrust.${k}`)}</dt>
                  <dd>{keys[k] ?? '…'}</dd>
                </div>
              ))}
            </dl>
            {keys.error && <InlineNotification kind="error" lowContrast hideCloseButton title={keys.error} />}
          </Stack>
        </Modal>
      )}
    </>
  );
}
