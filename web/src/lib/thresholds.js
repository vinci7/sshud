// Threshold levels: CPU and memory warn at 70 % and alert at 90 %, disk at 80 / 90 %.
export const LIMITS = {
  cpu: [70, 90],
  mem: [70, 90],
  disk: [80, 90],
};

export const RANK = { ok: 0, warn: 1, alert: 2 };

export function level(kind, value) {
  if (value == null) return 'none';
  const [warn, alert] = LIMITS[kind];
  if (value >= alert) return 'alert';
  if (value >= warn) return 'warn';
  return 'ok';
}

// Disk shown on the overview: the fullest real filesystem, so a full data
// volume is never hidden behind a roomy root.
export function mainDisk(disks) {
  if (!disks?.length) return null;
  return disks.reduce((a, b) => (b.percent > a.percent ? b : a));
}

export function worst(levels) {
  return levels.reduce((a, b) => ((RANK[b] ?? -1) > (RANK[a] ?? -1) ? b : a), 'none');
}
