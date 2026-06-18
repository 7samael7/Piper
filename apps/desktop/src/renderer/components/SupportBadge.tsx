import type { SupportLevel } from "@piper/shared-types";

interface SupportBadgeProps {
  support: SupportLevel;
}

export function SupportBadge({ support }: SupportBadgeProps) {
  return <span className={`support-badge support-${support}`}>{support}</span>;
}
