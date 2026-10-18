import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useTimezone } from "../../../context/timezone-context";
import { formatDateToTimezone } from "../../../lib/formatDateToTimezone";

// isStale is true while the watchdog counts the monitor as stale and no newer
// heartbeat has arrived since (heartbeats reach the page live, the monitor doesn't)
export function isStale(staleSince?: string, lastHeartbeatTime?: string): boolean {
  if (!staleSince) return false;
  if (!lastHeartbeatTime) return true;
  return new Date(lastHeartbeatTime).getTime() <= new Date(staleSince).getTime();
}

// StaleMarker shows that Peekaping has stopped checking a monitor
const StaleMarker = ({
  staleSince,
  lastHeartbeatTime,
  className,
}: {
  staleSince?: string;
  lastHeartbeatTime?: string;
  className?: string;
}) => {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();

  if (!staleSince || !isStale(staleSince, lastHeartbeatTime)) return null;

  return (
    <div
      className={cn("flex items-center gap-1 text-sm text-amber-500", className)}
      data-testid="stale-marker"
    >
      <AlertTriangle className="w-4 h-4 shrink-0" />
      {t("monitors.stale.not_checked_since", {
        time: formatDateToTimezone(staleSince, timezone),
      })}
    </div>
  );
};

export default StaleMarker;
