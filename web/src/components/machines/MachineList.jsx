import { Button, OverflowMenu, OverflowMenuItem, TableToolbar, TableToolbarContent } from '@carbon/react';
import { Add, DocumentImport } from '@carbon/icons-react';
import SimpleTable from '../SimpleTable.jsx';
import { t } from '../../lib/i18n.js';
import { machinePath, onLinkClick } from '../../lib/router.js';

// Address shows user@host:port, breakable after the "@" on narrow screens.
function Address({ m }) {
  if (!m.user) return <span className="sb-mono">{m.host}</span>;
  return (
    <span className="sb-mono">
      {m.user}@<wbr />
      {m.host}:{m.port}
    </span>
  );
}

// MachineList is the management table: name, address and an actions menu, with import / add buttons in the toolbar.
export default function MachineList({ list, readOnly, onAdd, onImport, onEdit, onSetup, onDelete }) {
  const headers = [
    { key: 'name', header: t('manage.name') },
    { key: 'address', header: t('manage.address') },
    { key: 'actions', header: '' },
  ];
  const rows = list.map((m) => ({
    id: m.id,
    name: (
      <a href={machinePath(m.id)} onClick={onLinkClick} className="sb-machines__name" title={m.name}>
        {m.name}
      </a>
    ),
    address: <Address m={m} />,
    actions: readOnly ? null : (
      <OverflowMenu size="sm" flipped aria-label={t('manage.actions')}>
        <OverflowMenuItem itemText={t('manage.setup')} onClick={() => onSetup(m)} />
        <OverflowMenuItem itemText={t('manage.edit')} onClick={() => onEdit(m)} />
        <OverflowMenuItem itemText={t('manage.delete')} isDelete hasDivider onClick={() => onDelete(m)} />
      </OverflowMenu>
    ),
  }));
  const toolbar = (
    <TableToolbar>
      <TableToolbarContent>
        <Button kind="ghost" renderIcon={DocumentImport} disabled={readOnly} onClick={onImport}>
          {t('import.open')}
        </Button>
        <Button renderIcon={Add} disabled={readOnly} onClick={onAdd}>
          {t('manage.add')}
        </Button>
      </TableToolbarContent>
    </TableToolbar>
  );
  return <SimpleTable headers={headers} rows={rows} size="lg" toolbar={toolbar} className="sb-machines" />;
}
