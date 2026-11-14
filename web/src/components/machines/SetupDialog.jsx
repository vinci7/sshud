import { useState } from 'react';
import { Button, CodeSnippet, Modal, Stack } from '@carbon/react';
import TestOutcome from './TestOutcome.jsx';
import { api } from '../../lib/api.js';
import { t } from '../../lib/i18n.js';

// SetupDialog connects a saved machine: install sshud's authorized_keys
// line (with a command the user runs on their own computer, or by hand),
// then test the connection and compare the host key fingerprint.
export default function SetupDialog({ machine, onClose }) {
  const [test, setTest] = useState(null);
  const runTest = () => {
    setTest({ running: true });
    api('POST', '/api/admin/test', machine).then(setTest, (e) => setTest({ ok: false, kind: 'internal', message: e.message }));
  };
  const snippet = (text) => (
    <CodeSnippet
      type="multi"
      wrapText
      maxCollapsedNumberOfRows={3}
      feedback={t('strict.copied')}
      showMoreText={t('strict.showMore')}
      showLessText={t('strict.showLess')}
    >
      {text}
    </CodeSnippet>
  );
  return (
    <Modal
      open
      hasScrollingContent
      modalHeading={t('setup.title', { name: machine.name })}
      primaryButtonText={t('setup.done')}
      onRequestSubmit={onClose}
      onRequestClose={onClose}
    >
      <Stack gap={5}>
        <p>{t('setup.intro')}</p>
        <p className="sb-muted">{t('setup.command')}</p>
        {snippet(machine.command)}
        <p className="sb-muted">{t('setup.line')}</p>
        {snippet(machine.line)}
        <p className="sb-muted">{t('strict.reads')}</p>
        <div>
          <Button kind="tertiary" size="sm" onClick={runTest} disabled={test?.running}>
            {t('manage.test')}
          </Button>
        </div>
        <TestOutcome result={test} />
      </Stack>
    </Modal>
  );
}
