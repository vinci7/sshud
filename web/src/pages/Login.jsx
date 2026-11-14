import { useState } from 'react';
import { Button, Form, InlineNotification, PasswordInput, Stack, Tile } from '@carbon/react';
import { t } from '../lib/i18n.js';
import { useTitle } from '../lib/router.js';

// Login doubles as first-run setup: with setup=true it asks for a new
// password twice and stores it.
export default function Login({ setup, onDone }) {
  useTitle(t('login.title'));
  const [pw, setPw] = useState('');
  const [pw2, setPw2] = useState('');
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e) => {
    e.preventDefault();
    setError(null);
    if (setup && pw !== pw2) {
      setError(t('login.mismatch'));
      return;
    }
    setBusy(true);
    try {
      const r = await fetch(setup ? '/api/setup' : '/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: pw }),
      });
      if (r.ok) {
        onDone();
        return;
      }
      const data = await r.json().catch(() => ({}));
      setError(r.status === 401 ? t('login.wrong') : data.error || r.statusText);
    } catch {
      setError(t('login.unreachable'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Tile className="sb-login">
      <h2 className="sb-login__title">{setup ? t('login.setupTitle') : t('login.title')}</h2>
      {setup && <p className="sb-muted">{t('login.setupBody')}</p>}
      <Form onSubmit={submit}>
        <Stack gap={5}>
          <PasswordInput
            id="pw"
            labelText={setup ? t('login.newPassword') : t('login.password')}
            helperText={setup ? t('login.minLength') : undefined}
            value={pw}
            onChange={(e) => setPw(e.target.value)}
            autoComplete={setup ? 'new-password' : 'current-password'}
            autoFocus
          />
          {setup && (
            <PasswordInput
              id="pw2"
              labelText={t('login.repeat')}
              value={pw2}
              onChange={(e) => setPw2(e.target.value)}
              autoComplete="new-password"
            />
          )}
          {error && <InlineNotification kind="error" lowContrast hideCloseButton title={error} />}
          <Button type="submit" disabled={busy || !pw}>
            {setup ? t('login.create') : t('login.submit')}
          </Button>
        </Stack>
      </Form>
    </Tile>
  );
}
