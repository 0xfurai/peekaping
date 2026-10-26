import type { ReactNode } from "react";
import type {
  ProbeProbeAvailabilityInstance,
  ProbeProbeAvailabilityResponse,
} from "@/api/types.gen";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useTimezone } from "@/context/timezone-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { cn } from "@/lib/utils";
import {
  formatDuration,
  placeSpans,
  placeUntracked,
  type Placement,
  instanceName,
} from "./availability-layout";

// A span too short to see still gets this much of the bar, so that a short gap shows
const MIN_SPAN_PX = 3;

interface Segment extends Placement {
  key: string;
  className: string;
  testId: string;
  tooltip: ReactNode;
}

interface TimeBarProps {
  segments: Segment[];
  size: "compact" | "full";
  label: string;
  testId: string;
  className?: string;
}

// A track with spans placed on it by time. The colours are tailwind's, with a dark variant, and
// the muted tokens, so the bar reads in both themes.
const TimeBar = ({
  segments,
  size,
  label,
  testId,
  className,
}: TimeBarProps) => (
  <div
    role="img"
    aria-label={label}
    data-testid={testId}
    className={cn(
      "relative overflow-hidden bg-muted",
      size === "compact" ? "h-2 w-32 rounded-full" : "h-6 w-full rounded-md",
      className
    )}
  >
    {segments.map((segment) => (
      <Tooltip key={segment.key}>
        <TooltipTrigger asChild>
          <div
            tabIndex={0}
            data-testid={segment.testId}
            className={cn("absolute inset-y-0", segment.className)}
            style={{
              left: `min(${segment.leftPct}%, calc(100% - ${MIN_SPAN_PX}px))`,
              width: `${segment.widthPct}%`,
              minWidth: MIN_SPAN_PX,
            }}
          />
        </TooltipTrigger>
        <TooltipContent>{segment.tooltip}</TooltipContent>
      </Tooltip>
    ))}
  </div>
);

const ONLINE = "bg-green-600 dark:bg-green-500";
const OFFLINE = "bg-red-600 dark:bg-red-500";
const NO_DATA = "bg-muted-foreground/25";

const useFormatTime = () => {
  const { timezone } = useTimezone();
  return (time: string) => formatDateToTimezone(time, timezone);
};

interface AvailabilityBarProps {
  availability: Pick<
    ProbeProbeAvailabilityResponse,
    "from" | "to" | "tracked_from" | "periods"
  >;
  size?: "compact" | "full";
}

// A probe's online and offline periods over the range, red where it was offline and muted where
// nothing was tracked.
export const AvailabilityBar = ({
  availability,
  size = "full",
}: AvailabilityBarProps) => {
  const { t } = useLocalizedTranslation();
  const formatTime = useFormatTime();
  const { from, to, tracked_from: trackedFrom, periods } = availability;

  const segments: Segment[] = [];

  const untracked = placeUntracked(from, to, trackedFrom);
  if (untracked) {
    segments.push({
      ...untracked,
      key: "no-data",
      className: NO_DATA,
      testId: "availability-span-no-data",
      tooltip: t("probes.availability.no_data"),
    });
  }

  // Offline goes last, so that a gap too short to see is widened over the online span next to it
  // and not under it. The server's periods are in time order, so the order of the rest is kept.
  const placed = placeSpans(from, to, periods);
  const online = placed.filter((period) => period.status === "online");
  const offline = placed.filter((period) => period.status === "offline");
  for (const period of [...online, ...offline]) {
    segments.push({
      leftPct: period.leftPct,
      widthPct: period.widthPct,
      key: `${period.status}-${period.start}`,
      className: period.status === "online" ? ONLINE : OFFLINE,
      testId: `availability-span-${period.status}`,
      tooltip: (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">
            {period.status === "online"
              ? t("probes.table.status.online")
              : t("probes.table.status.offline")}
          </span>
          <span>
            {formatTime(period.start)} – {formatTime(period.end)}
          </span>
          <span>
            {formatDuration(Date.parse(period.end) - Date.parse(period.start))}
          </span>
        </div>
      ),
    });
  }

  return (
    <TimeBar
      segments={segments}
      size={size}
      label={t("probes.availability.bar_label")}
      testId="availability-bar"
    />
  );
};

interface InstanceBarProps {
  instance: ProbeProbeAvailabilityInstance;
  from: string;
  to: string;
}

// When one probe instance was connected, over the last 24 hours: green while connected, the track
// where it was not.
export const InstanceBar = ({ instance, from, to }: InstanceBarProps) => {
  const { t } = useLocalizedTranslation();
  const formatTime = useFormatTime();

  const segments: Segment[] = placeSpans(from, to, instance.connections).map(
    (connection, index) => ({
      leftPct: connection.leftPct,
      widthPct: connection.widthPct,
      // Two requests after a quiet minute can each start a connection at the same time
      key: `${connection.start}-${index}`,
      className: ONLINE,
      testId: "instance-connection",
      tooltip: (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{instanceName(instance)}</span>
          <span>{instance.version}</span>
          <span>
            {connection.open
              ? t("probes.availability.connected_since", {
                  start: formatTime(connection.start),
                })
              : `${formatTime(connection.start)} – ${formatTime(connection.end)}`}
          </span>
        </div>
      ),
    })
  );

  return (
    <TimeBar
      segments={segments}
      size="full"
      testId="instance-bar"
      label={t("probes.availability.instance_bar_label", {
        name: instanceName(instance),
      })}
    />
  );
};
