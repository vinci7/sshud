// Ring is a full-circle gauge drawn to match Carbon Charts' gauge: thin
// stroke, square ends, value in IBM Plex at the centre. Colour encodes the
// threshold level only.
const SIZE = 64;
const STROKE = 6;
const R = (SIZE - STROKE) / 2;
const C = 2 * Math.PI * R;

export default function Ring({ value, level, label }) {
  const v = value == null ? 0 : Math.max(0, Math.min(100, value));
  return (
    <figure className="sb-ring" data-level={level}>
      <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} role="img" aria-label={`${label} ${value == null ? 'n/a' : Math.round(value)}`}>
        <circle className="sb-ring__track" cx={SIZE / 2} cy={SIZE / 2} r={R} strokeWidth={STROKE} fill="none" />
        <circle
          className="sb-ring__value"
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={R}
          strokeWidth={STROKE}
          fill="none"
          strokeDasharray={`${(v / 100) * C} ${C}`}
          transform={`rotate(-90 ${SIZE / 2} ${SIZE / 2})`}
        />
        <text className="sb-ring__text" x="50%" y="50%" dominantBaseline="central" textAnchor="middle">
          {value == null ? '—' : Math.round(value)}
        </text>
      </svg>
      <figcaption className="sb-ring__label" title={label}>{label}</figcaption>
    </figure>
  );
}
