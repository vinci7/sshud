import { useState } from 'react';
import { ContentSwitcher, InlineNotification, Link, Modal, PasswordInput, Stack, Switch, TextArea, TextInput } from '@carbon/react';
import TestOutcome from './TestOutcome.jsx';
import { api } from '../../lib/api.js';
import { t } from '../../lib/i18n.js';

export const EMPTY_MACHINE = { name: '', host: '', port: 22, user: '', from: '' };
const NO_CRED = { mode: 'password', password: '', privateKey: '', passphrase: '' };

// MachineForm edits a machine, or adds one in a single form with a one-time
// password or key that sshud uses to install its line, then done.
// onSaved(machine, manual) closes it; manual asks for the hand-install dialog.
export default function MachineForm({ initial, onClose, onSaved }) {
  const [m, setM] = useState(initial);
  const [cred, setCred] = useState(NO_CRED);
  const [created, setCreated] = useState(null);
  const [done, setDone] = useState(false);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  const editing = Boolean(initial.id);
  const set = (k) => (e) => setM({ ...m, [k]: k === 'port' ? Number(e.target.value) || '' : e.target.value });
  const setC = (k) => (e) => setCred({ ...cred, [k]: e.target.value });

  const run = (fn) => async () => {
    setBusy(true);
    setError(null);
    await fn().catch(setError);
    setBusy(false);
  };
  // create adds the machine once; a retry after a failed authorization
  // saves any fields changed since instead of reusing the stale record
  const create = async () => {
    const changed = created && Object.keys(EMPTY_MACHINE).some((k) => (created[k] ?? '') !== (m[k] ?? ''));
    const saved = !created
      ? await api('POST', '/api/admin/machines', m)
      : changed
        ? await api('PUT', `/api/admin/machines/${encodeURIComponent(created.id)}`, m)
        : created;
    setCreated(saved);
    return saved;
  };
  const submit = async () => {
    if (editing) return onSaved(await api('PUT', `/api/admin/machines/${encodeURIComponent(m.id)}`, m));
    if (done) return onSaved(created);
    const { mode, password, privateKey, passphrase } = cred;
    const saved = await create();
    await api('POST', `/api/admin/machines/${encodeURIComponent(saved.id)}/bootstrap`, mode === 'password' ? { password } : { privateKey, passphrase });
    setCred(NO_CRED); // drop the credentials as soon as they are used
    setDone(true);
  };
  const adding = !editing && !done;
  const missing = adding && !(cred.mode === 'password' ? cred.password : cred.privateKey);

  return (
    <Modal
      open
      modalHeading={editing ? t('manage.editTitle', { name: initial.name }) : t('manage.addTitle')}
      primaryButtonText={editing ? t('manage.save') : t(done ? 'setup.done' : 'boot.install')}
      primaryButtonDisabled={busy || missing}
      secondaryButtonText={done ? undefined : t('manage.cancel')}
      onRequestClose={created ? () => onSaved(created) : onClose}
      onRequestSubmit={run(submit)}
      size="sm"
      hasScrollingContent
    >
      <Stack gap={5}>
        {done ? (
          <InlineNotification kind="success" lowContrast hideCloseButton title={t('boot.done', { name: m.name })} />
        ) : (
          <>
            <TextInput id="m-name" labelText={t('manage.name')} value={m.name} onChange={set('name')} placeholder="web-1" />
            <div className="sb-form-row">
              <TextInput id="m-host" labelText={t('manage.host')} value={m.host} onChange={set('host')} placeholder="192.0.2.10" />
              <TextInput id="m-port" labelText={t('manage.port')} value={m.port} onChange={set('port')} inputMode="numeric" />
            </div>
            <TextInput id="m-user" labelText={t('manage.user')} value={m.user} onChange={set('user')} placeholder="ubuntu" />
            <TextInput
              id="m-from"
              labelText={t('strict.from')}
              helperText={t('strict.fromHelp')}
              value={m.from ?? ''}
              onChange={set('from')}
              placeholder="192.0.2.0/24"
            />
          </>
        )}
        {adding && (
          <>
            <ContentSwitcher size="sm" selectedIndex={cred.mode === 'key' ? 1 : 0} onChange={({ name }) => setCred({ ...cred, mode: name })}>
              <Switch name="password" text={t('boot.password')} />
              <Switch name="key" text={t('boot.key')} />
            </ContentSwitcher>
            {cred.mode === 'password' ? (
              <PasswordInput id="b-pw" labelText={t('boot.password')} value={cred.password} onChange={setC('password')} autoComplete="off" />
            ) : (
              <>
                <TextArea id="b-key" labelText={t('boot.key')} rows={5} value={cred.privateKey} onChange={setC('privateKey')} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
                <PasswordInput id="b-pp" labelText={t('boot.passphrase')} value={cred.passphrase} onChange={setC('passphrase')} autoComplete="off" />
              </>
            )}
            <p className="sb-muted">{t('boot.once')}</p>
          </>
        )}
        {error?.kind && <TestOutcome result={{ ok: false, kind: error.kind, message: error.message }} />}
        {error && !error.kind && <InlineNotification kind="error" lowContrast hideCloseButton title={t('manage.saveFailed')} subtitle={error.message} />}
        {adding && (
          <p className="sb-muted">
            <Link href="#" onClick={(e) => (e.preventDefault(), run(async () => onSaved(await create(), true))())}>
              {t('boot.manual')}
            </Link>
          </p>
        )}
      </Stack>
    </Modal>
  );
}
