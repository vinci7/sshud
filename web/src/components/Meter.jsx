// Meter is a flat horizontal bar of one or more segments, values in percent.
// Colours come from the threshold level or an explicit segment kind.
export default function Meter({ segments, label }) {
  return (
    <div className="sb-meter" role="img" aria-label={label}>
      {segments.map((s, i) => (
        <span key={i} className="sb-meter__seg" data-kind={s.kind} style={{ width: `${Math.max(0, Math.min(100, s.value))}%` }} />
      ))}
    </div>
  );
}
