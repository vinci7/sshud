import { DataTable, Table, TableBody, TableCell, TableContainer, TableHead, TableHeader, TableRow } from '@carbon/react';

// SimpleTable renders Carbon's DataTable for rows whose cells are already
// React nodes. `toolbar` goes above the table, inside its container.
export default function SimpleTable({ headers, rows, empty, size = 'sm', toolbar, className }) {
  if (!rows.length && empty) return <p className="sb-muted">{empty}</p>;
  return (
    <DataTable rows={rows} headers={headers} size={size}>
      {({ rows: r, headers: h, getTableProps, getHeaderProps, getRowProps }) => (
        <TableContainer className={className}>
          {toolbar}
          <Table {...getTableProps()}>
            <TableHead>
              <TableRow>
                {h.map((x) => (
                  <TableHeader key={x.key} {...getHeaderProps({ header: x })}>
                    {x.header}
                  </TableHeader>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {r.map((row) => (
                <TableRow key={row.id} {...getRowProps({ row })}>
                  {row.cells.map((c) => (
                    <TableCell key={c.id}>{c.value}</TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </DataTable>
  );
}
