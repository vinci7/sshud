import { useState } from 'react';
import { InlineLoading, InlineNotification, Modal, Stack, TextArea } from '@carbon/react';
import ImportPreview from './ImportPreview.jsx';
import { api } from '../../lib/api.js';
import { t } from '../../lib/i18n.js';

// ImportDialog turns pasted ssh_config text into machines: paste, preview
// with ready rows pre-selected, then import the selection.
export default function ImportDialog({ onClose, onImported }) {
  const [text, setText] = useState('');
  const [preview, setPreview] = useState(null);
  const [selected, setSelected] = useState({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [done, setDone] = useState(null);

  const runPreview = async () => {
    setError(null);
    try {
      const p = await api('POST', '/api/admin/import/preview', { text });
      setPreview(p);
      setSelected(Object.fromEntries(p.rows.map((r) => [r.name, !r.note])));
    } catch (e) {
      setError(e.message);
    }
  };
  const runImport = async () => {
    setBusy(true);
    const failed = [];
    let ok = 0;
    for (const r of preview.rows.filter((x) => selected[x.name])) {
      try {
        await api('POST', '/api/admin/machines', { name: r.name, host: r.host, port: r.port, user: r.user });
        ok++;
      } catch (e) {
        failed.push(`${r.name}: ${e.message}`);
      }
    }
    setBusy(false);
    setDone({ ok, failed });
    if (ok) onImported();
  };

  const count = Object.values(selected).filter(Boolean).length;
  const rows = preview?.rows ?? [];
  return (
    <Modal
      open
      size="lg"
      hasScrollingContent
      modalHeading={t('import.title')}
      primaryButtonText={done ? t('import.close') : preview ? t('import.run', { n: count }) : t('import.preview')}
      primaryButtonDisabled={busy || (!done && !preview && !text.trim()) || (preview && !done && count === 0)}
      secondaryButtonText={t('manage.cancel')}
      onRequestClose={onClose}
      onRequestSubmit={done ? onClose : preview ? runImport : runPreview}
    >
      <Stack gap={5}>
        {!preview ? (
          <TextArea
            id="import-text"
            labelText={t('import.label')}
            helperText={t('import.help')}
            rows={12}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={'Host web-1\n    HostName 192.0.2.10\n    User ubuntu'}
            className="sb-mono"
          />
        ) : rows.length === 0 ? (
          <InlineNotification kind="warning" lowContrast hideCloseButton title={t('import.none')} />
        ) : (
          <ImportPreview rows={rows} selected={selected} setSelected={setSelected} locked={busy || Boolean(done)} />
        )}
        {preview?.skipped?.length > 0 && (
          <p className="sb-muted">{t('import.skipped', { list: preview.skipped.join(', ') })}</p>
        )}
        {busy && <InlineLoading description={t('import.importing')} />}
        {done && (
          <InlineNotification
            kind={done.failed.length ? 'warning' : 'success'}
            lowContrast
            hideCloseButton
            title={t('import.done', { n: done.ok })}
            subtitle={done.failed.join('; ')}
          />
        )}
        {error && <InlineNotification kind="error" lowContrast hideCloseButton title={error} />}
      </Stack>
    </Modal>
  );
}
