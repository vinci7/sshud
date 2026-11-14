import { InlineLoading, InlineNotification } from '@carbon/react';
import { t } from '../../lib/i18n.js';

// TestOutcome shows the result of "Test connection": spinner, success with
// the host key fingerprint to compare, or the classified failure with a hint.
export default function TestOutcome({ result }) {
  if (!result) return null;
  if (result.running) return <InlineLoading description={t('manage.testing')} />;
  if (result.ok) {
    return (
      <InlineNotification
        kind="success"
        lowContrast
        hideCloseButton
        title={t('manage.testOk', { host: result.hostname, s: (result.ms / 1000).toFixed(1) })}
        subtitle={`${result.os ?? ''} · ${t('manage.fingerprint')} ${result.fingerprint ?? '—'}. ${t('manage.fingerprintCheck')}`}
      />
    );
  }
  const kind = result.kind ?? 'internal';
  return (
    <InlineNotification
      kind="error"
      lowContrast
      hideCloseButton
      title={t(`error.${kind}`)}
      subtitle={`${t(`hint.${kind}`)} (${result.message})`}
    />
  );
}
