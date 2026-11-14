import { platform } from '../lib/platform.js';

// PlatformIcon draws the platform() icon in one colour, whether it is a
// Carbon icon or a Simple Icons path (24×24 viewBox).
export default function PlatformIcon({ snapshot, size = 20 }) {
  const { icon: Icon, label } = platform(snapshot);
  const title = label || snapshot?.host?.os;
  if (!Icon.path) return <Icon size={size} className="sb-platform" aria-label={title} />;
  return (
    <svg className="sb-platform" width={size} height={size} viewBox="0 0 24 24" role="img" aria-label={title}>
      <path d={Icon.path} />
    </svg>
  );
}
