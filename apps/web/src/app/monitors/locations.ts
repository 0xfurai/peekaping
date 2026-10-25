import type { HeartbeatModel, MonitorModelLocation } from "@/api";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";

// The statuses a heartbeat records
const STATUS_DOWN = 0;
const STATUS_UP = 1;
const STATUS_PENDING = 2;
const STATUS_MAINTENANCE = 3;

// The local probe first, then the others by name. The API lists a monitor's probes by id, an order
// that means nothing to a reader.
const byProbe = (a: { id: string; name: string }, b: { id: string; name: string }): number => {
  if ((a.id === LOCAL_PROBE_ID) !== (b.id === LOCAL_PROBE_ID)) return a.id === LOCAL_PROBE_ID ? -1 : 1;
  return a.name.localeCompare(b.name);
};

// The probes a monitor runs on, with their names: the local probe first, then the others by name.
// A monitor with none is on the local probe, which is named even when the probes list is empty. A
// probe the list doesn't hold, such as one deleted since, shows its id. Probe names aren't unique,
// so the id is what tells two probes apart.
export const monitorProbes = (
  probeIds: string[] | null | undefined,
  probes: { id: string; name: string }[],
  localName: string
): { id: string; name: string }[] =>
  (probeIds?.length ? probeIds : [LOCAL_PROBE_ID])
    .map((id) => {
      const probe = probes.find((candidate) => candidate.id === id);
      return { id, name: probe ? probe.name : id === LOCAL_PROBE_ID ? localName : id };
    })
    .sort(byProbe);

// The names of the probes a monitor runs on, in the order of monitorProbes
export const probeNames = (
  probeIds: string[] | null | undefined,
  probes: { id: string; name: string }[],
  localName: string
): string[] => monitorProbes(probeIds, probes, localName).map((probe) => probe.name);

// A monitor's locations in the order the page shows them: the local probe first, then the others
// by name
export const sortLocations = (locations: MonitorModelLocation[]): MonitorModelLocation[] =>
  [...locations].sort((a, b) =>
    byProbe({ id: a.probe_id, name: a.probe_name }, { id: b.probe_id, name: b.probe_name })
  );

// Whether the monitor has locations to show one by one: it runs on more than one probe. With one
// probe the monitor page is the monitor's own.
export const hasLocations = (locations: MonitorModelLocation[] | null | undefined): boolean =>
  (locations?.length ?? 0) > 1;

export const heartbeatsOfProbe = (heartbeats: HeartbeatModel[], probeId: string): HeartbeatModel[] =>
  heartbeats.filter((heartbeat) => heartbeat.probe_id === probeId);

// The heartbeats a page shows for a probe: the ones it fetched, then the ones that arrived live
// since, minus the ones a later fetch returned too. Oldest first by time, so a live heartbeat never
// sits out of order, and at most the newest `limit` of them.
export const mergeHeartbeats = (fetched: HeartbeatModel[], live: HeartbeatModel[], limit: number): HeartbeatModel[] => {
  const known = new Set(fetched.map((heartbeat) => heartbeat.id));
  return [...fetched, ...live.filter((heartbeat) => !known.has(heartbeat.id))]
    .sort((a, b) => Date.parse(a.time) - Date.parse(b.time))
    .slice(-limit);
};

// A location's status: its latest heartbeat's, else the one the monitor reported when it loaded
export const currentStatus = (
  location: Pick<MonitorModelLocation, "status">,
  heartbeats: HeartbeatModel[]
): number | undefined => heartbeats.at(-1)?.status ?? location.status ?? undefined;

// How many of a monitor's locations must be Down for the monitor to be Down
export type LocationRule = "any" | "all" | "at_least";

// The monitor's status from its locations, as the server judges it (monitorStatus in the ingester):
// Down while the location rule is met by the online locations that have a status, otherwise
// Maintenance while any of them is in maintenance, otherwise Up while any is Up, otherwise Pending.
// Any is met by one Down location, all by every one of them being Down (and at least one counting),
// at_least by `threshold` of them. A location with no status yet, and one whose probe is offline,
// counts for nothing, neither as Down nor in the number "all" is measured against.
export const monitorStatusOf = (
  locations: { online: boolean; status?: number | null }[],
  rule: LocationRule,
  threshold: number | null | undefined
): number => {
  let counted = 0;
  let down = 0;
  let anyUp = false;
  let anyMaintenance = false;
  for (const { online, status } of locations) {
    if (!online || status === undefined || status === null) continue;
    counted++;
    if (status === STATUS_DOWN) down++;
    if (status === STATUS_UP) anyUp = true;
    if (status === STATUS_MAINTENANCE) anyMaintenance = true;
  }
  // At least needs a threshold that is met outright, so that none never takes a monitor down
  const met =
    rule === "all"
      ? counted > 0 && down === counted
      : rule === "at_least"
        ? !!threshold && threshold > 0 && down >= threshold
        : down > 0;
  if (met) return STATUS_DOWN;
  if (anyMaintenance) return STATUS_MAINTENANCE;
  return anyUp ? STATUS_UP : STATUS_PENDING;
};
