// Shared Carbon Charts options for the live CPU and network charts.
const timeFmt = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

// timeAxis puts three ticks inset from both ends of [from, to]: Carbon centres
// each label on its tick, so a tick at the edge pushes its label past the border.
export function timeAxis(from, to) {
  const span = to - from;
  return {
    mapsTo: 'date',
    scaleType: 'time',
    ticks: {
      values: [1 / 6, 1 / 2, 5 / 6].map((f) => new Date(from.getTime() + span * f)),
      rotation: 'never',
      formatter: (d) => timeFmt.format(d),
    },
  };
}

export function chartOptions(theme, extra) {
  return {
    theme,
    height: '160px',
    animations: false,
    toolbar: { enabled: false },
    legend: { enabled: false },
    points: { enabled: false },
    grid: { x: { enabled: false } },
    tooltip: { enabled: true },
    timeScale: { addSpaceOnEdges: 0 }, // the line runs to both edges, like the area chart
    ...extra,
  };
}
