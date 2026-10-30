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

// Whether the monitor is being checked. The server sends active: false for a monitor a person paused
// and for one whose type the install turned off, and the page can't tell the two apart.
export const isPublicMonitorChecked = (
  monitor: Pick<StatusPageMonitorWithHeartbeatsAndUptimeDto, "active">
): boolean => monitor.active !== false;

// The status a card shows for a monitor that isn't checked, in place of the last one it had. It is no
// monitor status, so it can't be taken for one of the four.
export const NOT_MONITORED = -1;

// The status to show for the monitor, or for one of its locations, which is checked as the monitor is
export const statusShownFor = <S extends number | undefined>(
  monitor: Pick<StatusPageMonitorWithHeartbeatsAndUptimeDto, "active">,
  status: S
): S | typeof NOT_MONITORED => (isPublicMonitorChecked(monitor) ? status : NOT_MONITORED);
