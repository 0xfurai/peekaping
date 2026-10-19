import { CheckCircle, Clock, Wrench, XCircle, type LucideIcon } from "lucide-react";

export interface MonitorStatusView {
  name: (t: (key: string) => string) => string;
  icon: LucideIcon;
  cardColor: string;
  pageColor: string;
}

// Colours are complete class names because Tailwind only picks up literals.
// Names are literal t() calls because i18n:parse deletes any key it can't find in one.
// Paused isn't here: the monitor decides it, not its latest heartbeat.
const monitorStatusViews: Partial<Record<number, MonitorStatusView>> = {
  0: {
    name: (t) => t("monitors.view.status.down"),
    icon: XCircle,
    cardColor: "text-red-500",
    pageColor: "text-red-400",
  },
  1: {
    name: (t) => t("monitors.view.status.up"),
    icon: CheckCircle,
    cardColor: "text-green-500",
    pageColor: "text-green-400",
  },
  2: {
    name: (t) => t("monitors.view.status.pending"),
    icon: Clock,
    cardColor: "text-orange-500",
    pageColor: "text-orange-400",
  },
  3: {
    name: (t) => t("monitors.view.status.maintenance"),
    icon: Wrench,
    cardColor: "text-blue-500",
    pageColor: "text-blue-400",
  },
};

// getMonitorStatusView returns undefined for a missing or unknown status
export function getMonitorStatusView(
  status?: number
): MonitorStatusView | undefined {
  return status === undefined ? undefined : monitorStatusViews[status];
}
