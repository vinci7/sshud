import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow, TableSelectAll, TableSelectRow, Tag } from '@carbon/react';
import { t } from '../../lib/i18n.js';

// ImportPreview lists parsed ssh_config hosts with a checkbox each. Hosts
// that already exist cannot be selected.
export default function ImportPreview({ rows, selected, setSelected, locked }) {
  const count = Object.values(selected).filter(Boolean).length;
  const selectable = rows.filter((r) => r.note !== 'exists').length;
  return (
    <Table size="sm">
      <TableHead>
        <TableRow>
          <TableSelectAll
            id="import-all"
            name="import-all"
            checked={count > 0 && count === selectable}
            indeterminate={count > 0 && count < selectable}
            onSelect={() => {
              const all = count === 0;
              setSelected(Object.fromEntries(rows.map((r) => [r.name, all && r.note !== 'exists'])));
            }}
          />
          <TableHeader>{t('manage.name')}</TableHeader>
          <TableHeader>{t('manage.address')}</TableHeader>
          <TableHeader>{t('import.note')}</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.name}>
            <TableSelectRow
              id={`import-${r.name}`}
              name={`import-${r.name}`}
              checked={Boolean(selected[r.name])}
              disabled={r.note === 'exists' || locked}
              onSelect={() => setSelected({ ...selected, [r.name]: !selected[r.name] })}
            />
            <TableCell>{r.name}</TableCell>
            <TableCell className="sb-mono">{`${r.user || '?'}@${r.host}:${r.port}`}</TableCell>
            <TableCell>
              {r.note && (
                <Tag size="sm" type={r.note === 'exists' ? 'gray' : 'warm-gray'}>
                  {t(`import.note.${r.note}`)}
                </Tag>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
