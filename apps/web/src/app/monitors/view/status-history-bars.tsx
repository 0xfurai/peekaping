import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { getMonitorsByIdStatsPointsOptions } from "@/api/@tanstack/react-query.gen";
import type { MonitorModelLocation, MonitorStatPoint } from "@/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { NoData } from "@/components/no-data";
import { useTimezone } from "@/context/timezone-context";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { cn } from "@/lib/utils";

const ALL_LOCATIONS = "all-locations";
const MINUTE_MS = 60 * 1000;
const HOUR_MS = 60 * MINUTE_MS;

export const ALIGNED_BUCKET_COUNT = 48;

export type StatusHistoryPeriodKey = "24h" | "14d" | "30d";

export type BucketStatus =
  | "up"
  | "partial"
  | "down"
  | "maintenance"
  | "no_data";

export interface AggregatedBucket {
  startMs: number;
  endMs: number;
  up: number;
  down: number;
  maintenance: number;
  ping: number;
  ping_min: number | null;
  ping_max: number | null;
}

export interface StatusHistoryRangeParams {
  since: string;
  until: string;
  granularity: "minute" | "hour";
  bucketCount: number;
  bucketMs: number;
  windowStartMs: number;
  windowEndMs: number;
}

export function classifyBucket(
  point: Pick<MonitorStatPoint, "up" | "down" | "maintenance">
): BucketStatus {
  const up = point.up ?? 0;
  const down = point.down ?? 0;
  const maintenance = point.maintenance ?? 0;
  if (up === 0 && down === 0 && maintenance === 0) return "no_data";
  if (down > 0 && up === 0) return "down";
  if (down > 0 && up > 0) return "partial";
  if (maintenance > 0) return "maintenance";
  return "up";
}

export function bucketUptimePercent(
  point: Pick<MonitorStatPoint, "up" | "down">
): number | null {
  const up = point.up ?? 0;
  const down = point.down ?? 0;
  const total = up + down;
  if (total === 0) return null;
  return (up / total) * 100;
}

// Aligned 48-bucket layout across all 3 windows so every row starts and ends on the exact
// same vertical edge with pixel-identical spacing:
// - 24h: 48 x 30-minute buckets (from minute stats, <= 1440 points)
// - 14d: 48 x 7-hour buckets (from hour stats, <= 336 points)
// - 30d: 48 x 15-hour buckets (from hour stats, <= 720 points)
export function getStatusHistoryRangeParams(
  period: StatusHistoryPeriodKey,
  nowMs: number = Date.now()
): StatusHistoryRangeParams {
  const alignedNowMs = Math.floor(nowMs / MINUTE_MS) * MINUTE_MS;

  let bucketMs: number;
  let granularity: "minute" | "hour";

  switch (period) {
    case "24h":
      bucketMs = 30 * MINUTE_MS;
      granularity = "minute";
      break;
    case "14d":
      bucketMs = 7 * HOUR_MS;
      granularity = "hour";
      break;
    case "30d":
      bucketMs = 15 * HOUR_MS;
      granularity = "hour";
      break;
  }

  // Rightmost bucket holds the current time slot.
  const currentBucketStart = Math.floor(alignedNowMs / bucketMs) * bucketMs;
  const windowEndMs = currentBucketStart + bucketMs;
  const windowStartMs = windowEndMs - ALIGNED_BUCKET_COUNT * bucketMs;

  // Keep minute query within the backend's 1441-point cap (1439 minutes = 1440 points).
  const queryUntilMs =
    granularity === "minute"
      ? Math.min(alignedNowMs, windowStartMs + 1439 * MINUTE_MS)
      : alignedNowMs;

  return {
    since: new Date(windowStartMs).toISOString(),
    until: new Date(Math.max(windowStartMs, queryUntilMs)).toISOString(),
    granularity,
    bucketCount: ALIGNED_BUCKET_COUNT,
    bucketMs,
    windowStartMs,
    windowEndMs,
  };
}

// Groups fine-grained stat points into fixed-width buckets across [windowStartMs, windowEndMs).
export function aggregateIntoBuckets(
  points: MonitorStatPoint[],
  params: Pick<StatusHistoryRangeParams, "bucketCount" | "bucketMs" | "windowStartMs">
): AggregatedBucket[] {
  const acc = Array.from({ length: params.bucketCount }, (_, i) => ({
    startMs: params.windowStartMs + i * params.bucketMs,
    endMs: params.windowStartMs + (i + 1) * params.bucketMs,
    up: 0,
    down: 0,
    maintenance: 0,
    pingSum: 0,
    pingWeight: 0,
    pingMin: null as number | null,
    pingMax: null as number | null,
  }));

  for (const point of points) {
    if (point.timestamp == null) continue;
    const idx = Math.floor((point.timestamp - params.windowStartMs) / params.bucketMs);
    if (idx < 0 || idx >= params.bucketCount) continue;

    const slot = acc[idx];
    const up = point.up ?? 0;
    const down = point.down ?? 0;
    const maintenance = point.maintenance ?? 0;

    slot.up += up;
    slot.down += down;
    slot.maintenance += maintenance;

    // Maintenance checks are counted in `up` by the backend, but carry no response time.
    const pingWeight = Math.max(0, up - maintenance);
    if (pingWeight > 0 && (point.ping ?? 0) > 0) {
      slot.pingSum += (point.ping ?? 0) * pingWeight;
      slot.pingWeight += pingWeight;

      const pMin = point.ping_min ?? point.ping ?? 0;
      const pMax = point.ping_max ?? point.ping ?? 0;
      slot.pingMin = slot.pingMin === null ? pMin : Math.min(slot.pingMin, pMin);
      slot.pingMax = slot.pingMax === null ? pMax : Math.max(slot.pingMax, pMax);
    }
  }

  return acc.map((slot) => ({
    startMs: slot.startMs,
    endMs: slot.endMs,
    up: slot.up,
    down: slot.down,
    maintenance: slot.maintenance,
    ping: slot.pingWeight > 0 ? slot.pingSum / slot.pingWeight : 0,
    ping_min: slot.pingMin,
    ping_max: slot.pingMax,
  }));
}

// Computes exact device-pixel-aligned segment width, gap, and left offset so every bar
// and every gap are pixel-identical with zero subpixel rounding jitter across DPI scales
// (including Windows 125%/150% fractional scaling).
export function computeBarGeometry(
  containerWidth: number,
  count: number,
  devicePixelRatio: number = 1
): {
  segmentWidth: number;
  gap: number;
  stripWidth: number;
  offsetLeft: number;
} | null {
  if (containerWidth <= 0 || count <= 0) return null;
  const dpr =
    Number.isFinite(devicePixelRatio) && devicePixelRatio > 0
      ? devicePixelRatio
      : 1;
  const containerPx = Math.floor(containerWidth * dpr);
  const preferredGapPx = Math.max(1, Math.round(3 * dpr));
  const compactGapPx = Math.max(1, Math.round(2 * dpr));
  const minComfortSegmentPx = Math.max(1, Math.round(4 * dpr));

  const gapPx =
    Math.floor((containerPx - (count - 1) * preferredGapPx) / count) >=
    minComfortSegmentPx
      ? preferredGapPx
      : compactGapPx;
  const segmentPx = Math.max(
    1,
    Math.floor((containerPx - (count - 1) * gapPx) / count)
  );
  const stripPx = count * segmentPx + (count - 1) * gapPx;
  const offsetPx = Math.max(0, Math.floor((containerPx - stripPx) / 2));

  return {
    segmentWidth: segmentPx / dpr,
    gap: gapPx / dpr,
    stripWidth: stripPx / dpr,
    offsetLeft: offsetPx / dpr,
  };
}

// Inset Signal Panel bar styling: "no_data" sits as a quiet baseline dash while operational
// and incident bars rise above it like a signal meter.
const signalBarClass: Record<BucketStatus, string> = {
  up: "h-[22px] rounded-[2.5px] bg-green-500 group-hover:bg-green-400 transition-all duration-100 group-hover:-translate-y-0.5",
  partial:
    "h-[26px] rounded-[2.5px] bg-amber-500 group-hover:bg-amber-400 transition-all duration-100 group-hover:-translate-y-0.5",
  down: "h-[26px] rounded-[2.5px] bg-red-500 group-hover:bg-red-400 transition-all duration-100 group-hover:-translate-y-0.5",
  maintenance:
    "h-[18px] rounded-[2.5px] bg-blue-500 group-hover:bg-blue-400 transition-all duration-100 group-hover:-translate-y-0.5",
  no_data:
    "h-[6px] rounded-[2px] bg-zinc-300 dark:bg-zinc-800 group-hover:bg-zinc-400 dark:group-hover:bg-zinc-700 transition-colors",
};

const statusDotClass: Record<BucketStatus, string> = {
  up: "bg-green-500",
  partial: "bg-amber-500",
  down: "bg-red-500",
  maintenance: "bg-blue-500",
  no_data: "bg-zinc-400 dark:bg-zinc-600",
};

function uptimeTone(loaded: boolean, uptime: number | null | undefined) {
  if (!loaded || uptime == null) {
    return {
      dot: "bg-zinc-400 dark:bg-zinc-600",
      text: "text-muted-foreground",
    };
  }
  if (uptime >= 99.9) {
    return {
      dot: "bg-green-500",
      text: "text-emerald-600 dark:text-emerald-400",
    };
  }
  if (uptime > 0) {
    return {
      dot: "bg-amber-500",
      text: "text-amber-600 dark:text-amber-400",
    };
  }
  return {
    dot: "bg-red-500",
    text: "text-red-600 dark:text-red-400",
  };
}

interface PeriodRowProps {
  id: string;
  period: StatusHistoryPeriodKey;
  probeId?: string;
  nowMs: number;
  geometry: ReturnType<typeof computeBarGeometry>;
  title: string;
  stepLabel: string;
  startLabel: string;
  endLabel: string;
}

const PeriodRow: React.FC<PeriodRowProps> = ({
  id,
  period,
  probeId,
  nowMs,
  geometry,
  title,
  stepLabel,
  startLabel,
  endLabel,
}) => {
  const { timezone } = useTimezone();
  const { t } = useLocalizedTranslation();

  const params = React.useMemo(
    () => getStatusHistoryRangeParams(period, nowMs),
    [period, nowMs]
  );

  const { data } = useQuery({
    ...getMonitorsByIdStatsPointsOptions({
      path: { id },
      query: {
        since: params.since,
        until: params.until,
        granularity: params.granularity,
        probe_id: probeId,
      },
    }),
    refetchInterval: 60 * 1000,
    enabled: !!id,
  });

  const loaded = data?.data !== undefined;
  const overallUptime = data?.data?.uptime;
  const buckets = React.useMemo(
    () => aggregateIntoBuckets(data?.data?.points ?? [], params),
    [data?.data?.points, params]
  );

  const tone = uptimeTone(loaded, overallUptime);

  const formatBucketRange = (startMs: number, endMs: number) => {
    const startText = formatDateToTimezone(new Date(startMs), timezone, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
    const endText = formatDateToTimezone(new Date(endMs), timezone, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
    return `${startText} – ${endText}`;
  };

  const statusLabel = (status: BucketStatus) => {
    switch (status) {
      case "up":
        return t("monitors.view.status_history.operational");
      case "partial":
        return t("monitors.view.status_history.partial_outage");
      case "down":
        return t("monitors.view.status_history.downtime");
      case "maintenance":
        return t("monitors.view.status.maintenance");
      default:
        return t("monitors.view.status_history.no_data");
    }
  };

  const alignedBandStyle: React.CSSProperties | undefined = geometry
    ? {
        width: `${geometry.stripWidth}px`,
        marginLeft: `${geometry.offsetLeft}px`,
      }
    : undefined;

  const stripStyle: React.CSSProperties = geometry
    ? {
        display: "flex",
        alignItems: "flex-end",
        gap: `${geometry.gap}px`,
        width: `${geometry.stripWidth}px`,
        marginLeft: `${geometry.offsetLeft}px`,
      }
    : {
        display: "grid",
        gridTemplateColumns: `repeat(${params.bucketCount}, minmax(0, 1fr))`,
        alignItems: "end",
        gap: "3px",
        width: "100%",
      };

  const slotStyle: React.CSSProperties | undefined = geometry
    ? { width: `${geometry.segmentWidth}px`, flexShrink: 0 }
    : undefined;

  return (
    <div
      className="rounded-xl border border-border/60 bg-muted/30 dark:bg-zinc-900/60 px-4 py-3.5"
      data-testid={`status-history-row-${period}`}
    >
      <div
        className="flex items-center justify-between gap-2 mb-2.5"
        style={alignedBandStyle}
      >
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={cn("inline-block w-2 h-2 rounded-full shrink-0", tone.dot)}
            aria-hidden="true"
          />
          <span className="text-sm font-semibold truncate">{title}</span>
          <span className="text-xs text-muted-foreground hidden sm:inline">
            · {stepLabel}
          </span>
        </div>
        <span className={cn("text-[13px] font-semibold tabular-nums shrink-0", tone.text)}>
          {!loaded ? (
            <>&nbsp;</>
          ) : overallUptime == null ? (
            <NoData />
          ) : (
            `${overallUptime.toFixed(2)}% ${t("timezone.uptime").toLowerCase()}`
          )}
        </span>
      </div>

      <TooltipProvider delayDuration={100}>
        <div
          className="h-[26px]"
          style={stripStyle}
          role="list"
          aria-label={title}
        >
          {buckets.map((bucket) => {
            const status = loaded ? classifyBucket(bucket) : "no_data";
            const uptime = bucketUptimePercent(bucket);
            const hasPing = bucket.ping > 0;
            return (
              <Tooltip key={bucket.startMs}>
                <TooltipTrigger asChild>
                  <div
                    role="listitem"
                    data-testid={`status-history-bar-${period}`}
                    data-status={status}
                    style={slotStyle}
                    className="h-full flex items-end cursor-default group"
                  >
                    <div
                      className={cn(
                        "w-full",
                        loaded
                          ? signalBarClass[status]
                          : "h-[6px] rounded-[2px] bg-zinc-300/60 dark:bg-zinc-800/70 animate-pulse"
                      )}
                    />
                  </div>
                </TooltipTrigger>
                {loaded && (
                  <TooltipContent
                    sideOffset={6}
                    className="max-w-xs space-y-1 rounded-lg border border-border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-lg"
                    arrowClassName="bg-popover fill-popover border-r border-b border-border"
                  >
                    <div className="font-semibold text-popover-foreground">
                      {formatBucketRange(bucket.startMs, bucket.endMs)}
                    </div>
                    <div className="flex items-center gap-1.5 font-medium text-popover-foreground">
                      <span
                        className={cn(
                          "inline-block w-2 h-2 rounded-full shrink-0",
                          statusDotClass[status]
                        )}
                      />
                      <span>
                        {statusLabel(status)}
                        {uptime !== null ? ` · ${uptime.toFixed(2)}%` : ""}
                      </span>
                    </div>
                    {status !== "no_data" && (
                      <div className="text-muted-foreground">
                        {t("monitors.view.status.up")}: {bucket.up}
                        {bucket.down > 0 &&
                          ` · ${t("monitors.view.status.down")}: ${bucket.down}`}
                        {bucket.maintenance > 0 &&
                          ` · ${t("monitors.view.status.maintenance")}: ${bucket.maintenance}`}
                      </div>
                    )}
                    {hasPing && (
                      <div className="text-muted-foreground tabular-nums">
                        {t("timezone.average")}: {Math.round(bucket.ping)} ms
                        {bucket.ping_min != null && bucket.ping_max != null
                          ? ` (${Math.round(bucket.ping_min)}–${Math.round(bucket.ping_max)} ms)`
                          : ""}
                      </div>
                    )}
                  </TooltipContent>
                )}
              </Tooltip>
            );
          })}
        </div>
      </TooltipProvider>

      <div
        className="flex items-center justify-between mt-2 text-[11px] text-muted-foreground"
        style={alignedBandStyle}
      >
        <span>{startLabel}</span>
        <span>{endLabel}</span>
      </div>
    </div>
  );
};

export function StatusHistoryBars({
  id,
  locations,
}: {
  id: string;
  locations?: MonitorModelLocation[];
}) {
  const { t } = useLocalizedTranslation();
  const [location, setLocation] = React.useState(ALL_LOCATIONS);
  const [nowMs, setNowMs] = React.useState(() => Date.now());
  const trackMeasureRef = React.useRef<HTMLDivElement>(null);
  const [layoutMetrics, setLayoutMetrics] = React.useState({
    width: 0,
    dpr: 1,
  });

  // Advance the query window once a minute so the bars stay fresh without re-querying on every heartbeat.
  React.useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), 60 * 1000);
    return () => clearInterval(timer);
  }, []);

  React.useEffect(() => {
    const el = trackMeasureRef.current;
    if (!el) return;
    const measure = () =>
      setLayoutMetrics({
        width: el.getBoundingClientRect().width,
        dpr: window.devicePixelRatio || 1,
      });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  // One shared geometry guarantees all 3 inset panels start and end on the exact same pixel.
  const sharedGeometry = React.useMemo(
    () =>
      computeBarGeometry(
        layoutMetrics.width,
        ALIGNED_BUCKET_COUNT,
        layoutMetrics.dpr
      ),
    [layoutMetrics.width, layoutMetrics.dpr]
  );

  const probeId = locations?.some((l) => l.probe_id === location)
    ? location
    : undefined;

  return (
    <Card className="mb-4" data-testid="status-history-card">
      <CardHeader className="flex items-center gap-2 space-y-0 border-b sm:flex-row">
        <div className="grid flex-1 gap-1 text-center sm:text-left">
          <CardTitle>{t("monitors.view.status_history.title")}</CardTitle>
        </div>
        {locations && locations.length > 1 && (
          <Select value={probeId ?? ALL_LOCATIONS} onValueChange={setLocation}>
            <SelectTrigger
              className="w-[180px] rounded-lg sm:ml-auto"
              aria-label={t("monitors.view.locations.select_label")}
              data-testid="status-history-location-select"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="rounded-xl">
              <SelectItem value={ALL_LOCATIONS} className="rounded-lg">
                {t("monitors.view.locations.all")}
              </SelectItem>
              {locations.map((l) => (
                <SelectItem
                  key={l.probe_id}
                  value={l.probe_id}
                  className="rounded-lg"
                >
                  {l.probe_name || l.probe_id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </CardHeader>

      <CardContent className="pt-5">
        {/* Mirrors the inset panel horizontal border + padding so measured width matches 1:1 */}
        <div
          className="border border-transparent px-4 h-0 overflow-hidden pointer-events-none"
          aria-hidden="true"
        >
          <div ref={trackMeasureRef} className="w-full" />
        </div>

        <div className="space-y-3 w-full">
          <PeriodRow
            id={id}
            period="24h"
            probeId={probeId}
            nowMs={nowMs}
            geometry={sharedGeometry}
            title={t("monitors.view.stats.last_24_hours")}
            stepLabel={t("monitors.view.status_history.per_bar_minutes", {
              count: 30,
            })}
            startLabel={t("monitors.view.status_history.hours_ago", { count: 24 })}
            endLabel={t("monitors.view.status_history.now")}
          />
          <PeriodRow
            id={id}
            period="14d"
            probeId={probeId}
            nowMs={nowMs}
            geometry={sharedGeometry}
            title={t("monitors.view.stats.last_14_days")}
            stepLabel={t("monitors.view.status_history.per_bar_hours", {
              count: 7,
            })}
            startLabel={t("monitors.view.status_history.days_ago", { count: 14 })}
            endLabel={t("monitors.view.status_history.today")}
          />
          <PeriodRow
            id={id}
            period="30d"
            probeId={probeId}
            nowMs={nowMs}
            geometry={sharedGeometry}
            title={t("monitors.view.stats.last_30_days")}
            stepLabel={t("monitors.view.status_history.per_bar_hours", {
              count: 15,
            })}
            startLabel={t("monitors.view.status_history.days_ago", { count: 30 })}
            endLabel={t("monitors.view.status_history.today")}
          />

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pt-3 border-t text-xs text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-[2px] bg-green-500" />
              {t("monitors.view.status_history.operational")}
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-[2px] bg-amber-500" />
              {t("monitors.view.status_history.partial_outage")}
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-[2px] bg-red-500" />
              {t("monitors.view.status_history.downtime")}
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-[2px] bg-blue-500" />
              {t("monitors.view.status.maintenance")}
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-1 rounded-[1px] bg-zinc-300 dark:bg-zinc-700" />
              {t("monitors.view.status_history.no_data")}
            </span>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
