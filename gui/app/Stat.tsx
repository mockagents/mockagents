import { Icon, type IconName } from "@/lib/icons";

/** A design-system stat tile: icon + label, big value, optional sub-line. */
export function Stat({
  icon,
  label,
  value,
  sub,
}: {
  icon: IconName;
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="stat">
      <div className="k">
        <Icon name={icon} size={14} /> {label}
      </div>
      <div className="v">{value}</div>
      {sub && <div className="s">{sub}</div>}
    </div>
  );
}
