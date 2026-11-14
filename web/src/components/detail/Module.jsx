import { Tile } from '@carbon/react';
import { t } from '../../lib/i18n.js';

// Module is one titled tile on the detail page.
export default function Module({ title, aside, children, className = '' }) {
  return (
    <Tile className={`sb-module ${className}`}>
      <div className="sb-module__head">
        <h3 className="sb-module__title">{title}</h3>
        {aside && <span className="sb-module__aside">{aside}</span>}
      </div>
      {children}
    </Tile>
  );
}

export function Unsupported({ what }) {
  return <p className="sb-muted">{t('detail.unsupported', { what })}</p>;
}
