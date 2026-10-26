import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { HeartbeatModel, MonitorModelLocation } from "@/api";
import {
  getMonitorsByIdHeartbeatsOptions,
  getMonitorsByIdStatsUptimeOptions,
} from "@/api/@tanstack/react-query.gen";
import BarHistory from "@/components/bars";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { cn } from "@/lib/utils";
import { getMonitorStatusView } from "../components/monitor-status";
import { currentStatus, heartbeatsOfProbe, mergeHeartbeats } from "../locations";

// How many heartbeats a location's bar keeps
export const LOCATION_HEARTBEATS = 150;

type LocationRowProps = {
  monitorId: string;
  // The monitor's type, for saying which type the location's probe doesn't run
  monitorType: string;
  location: MonitorModelLocation;
  // Whether the location's probe doesn't run the monitor's type, so that it checks nothing
  unsupported: boolean;
  // The heartbeats the page received live since it loaded, of every location
  liveHeartbeats: HeartbeatModel[];
};

// One location of a monitor: the probe's name, its latest status and response time, its 24 hour
// uptime and its own heartbeat bar
const LocationRow = ({ monitorId, monitorType, location, unsupported, liveHeartbeats }: LocationRowProps) => {
  const { t } = useLocalizedTranslation();

  const { data: heartbeatsResponse } = useQuery({
    ...getMonitorsByIdHeartbeatsOptions({
      path: { id: monitorId },
      query: { limit: LOCATION_HEARTBEATS, reverse: true, probe_id: location.probe_id },
    }),
    staleTime: 0,
    refetchOnMount: true,
  });

  const live = useMemo(
    () => heartbeatsOfProbe(liveHeartbeats, location.probe_id),
    [liveHeartbeats, location.probe_id]
  );
  const heartbeats = useMemo(
    () => mergeHeartbeats(heartbeatsResponse?.data ?? [], live, LOCATION_HEARTBEATS),
    [heartbeatsResponse, live]
  );

  const { data: uptime, refetch: refetchUptime } = useQuery({
    ...getMonitorsByIdStatsUptimeOptions({
      path: { id: monitorId },
      query: { probe_id: location.probe_id },
    }),
  });

  // A new heartbeat of the location changes its uptime
  const latestLiveId = live.at(-1)?.id;
  useEffect(() => {
    if (latestLiveId) void refetchUptime();
  }, [latestLiveId, refetchUptime]);

  const latest = heartbeats.at(-1);
  const statusView = getMonitorStatusView(currentStatus(location, heartbeats));
  // The heartbeat has the response time of the latest check; the monitor's has it until one arrives
  const ping = latest?.ping ?? location.last_ping;
  const uptime24h = uptime?.data?.["24h"];

  return (
    <div className="space-y-2" data-testid="monitor-location">
      <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
        <span className="font-semibold" data-testid="monitor-location-name">
          {location.probe_name || location.probe_id}
        </span>
        <span
          className={cn("font-semibold", statusView?.pageColor)}
          data-testid="monitor-location-status"
        >
          {statusView ? statusView.name(t) : t("monitors.view.locations.no_result")}
        </span>
        <span className="text-sm text-gray-400" data-testid="monitor-location-ping">
          {t("monitors.view.locations.response_time")}:{" "}
          {ping === null || ping === undefined ? "-" : `${ping} ${t("timezone.ms")}`}
        </span>
        <span className="text-sm text-gray-400" data-testid="monitor-location-uptime">
          {t("monitors.view.stats.last_24_hours")}:{" "}
          {uptime24h === undefined ? "-" : `${uptime24h.toLocaleString()} %`}
        </span>
        {!location.online && (
          <span className="text-sm text-orange-400" data-testid="monitor-location-offline">
            {t("monitors.view.locations.probe_offline")}
          </span>
        )}
        {unsupported && (
          <span
            className="text-sm text-amber-500"
            title={t("probes.monitor_probe_unsupported", {
              name: location.probe_name || location.probe_id,
              type: monitorType,
            })}
            data-testid="monitor-location-unsupported"
          >
            {t("monitors.view.locations.probe_unsupported")}
          </span>
        )}
      </div>
      <BarHistory data={heartbeats} />
    </div>
  );
};

type LocationsSectionProps = {
  monitorId: string;
  monitorType: string;
  locations: MonitorModelLocation[];
  // The ids of the probes that don't run the monitor's type
  unsupportedProbeIds: string[];
  liveHeartbeats: HeartbeatModel[];
};

// The locations of a monitor that runs on more than one probe, each with its own heartbeat bar.
// One bar for the monitor would interleave the probes' heartbeats and mislead.
const LocationsSection = ({
  monitorId,
  monitorType,
  locations,
  unsupportedProbeIds,
  liveHeartbeats,
}: LocationsSectionProps) => {
  const { t } = useLocalizedTranslation();

  return (
    <div className="space-y-4" data-testid="monitor-locations">
      <div className="text-white font-semibold">{t("monitors.view.locations.title")}</div>
      {locations.map((location) => (
        <LocationRow
          key={location.probe_id}
          monitorId={monitorId}
          monitorType={monitorType}
          location={location}
          unsupported={unsupportedProbeIds.includes(location.probe_id)}
          liveHeartbeats={liveHeartbeats}
        />
      ))}
    </div>
  );
};

export default LocationsSection;
