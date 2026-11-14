import { ActionableNotification } from '@carbon/react';
import { t } from '../lib/i18n.js';
import { navigate } from '../lib/router.js';

// StaleProbeNotice lists the machines whose authorized_keys line was written
// for an older probe and links to Machines, where each has its new line.
export default function StaleProbeNotice({ machines, updates }) {
  const stale = machines.filter((m) => updates[m.id]?.snapshot?.probeOutdated);
  if (!stale.length) return null;
  return (
    <ActionableNotification
      kind="warning"
      inline
      lowContrast
      hideCloseButton
      className="sb-stale"
      title={t('stale.title', { n: stale.length })}
      subtitle={stale.map((m) => m.name).join(', ')}
      actionButtonLabel={t('outdated.action')}
      onActionButtonClick={() => navigate('/machines')}
    />
  );
}
