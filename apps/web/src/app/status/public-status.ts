import type { StatusPageMonitorWithHeartbeatsAndUptimeDto } from "@/api/types.gen";

// The status a visitor of a status page sees for a monitor: the monitor's own. A monitor on several
// probes has a heartbeat from each, so its latest heartbeat is only one probe's word. The latest
// heartbeat stands in for a response that carries no status.
export const publicMonitorStatus = (
  monitor: Pick<StatusPageMonitorWithHeartbeatsAndUptimeDto, "heartbeats"> &
    Partial<Pick<StatusPageMonitorWithHeartbeatsAndUptimeDto, "status">>
): number | undefined => monitor.status ?? monitor.heartbeats?.at(-1)?.status;

// Whether the monitor's card lists its locations one by one, as the page's setting and the monitor
// decide on the server: they come only for a page that shows them and a monitor on several probes.
export const hasPublicLocations = (
  monitor: Pick<StatusPageMonitorWithHeartbeatsAndUptimeDto, "locations">
): boolean => (monitor.locations?.length ?? 0) > 0;
