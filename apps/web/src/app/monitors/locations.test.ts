import { describe, expect, it } from "vitest";
import type { HeartbeatModel, MonitorModelLocation } from "@/api";
import {
  currentStatus,
  hasLocations,
  heartbeatsOfProbe,
  mergeHeartbeats,
  monitorStatusOf,
  monitorProbes,
  probeNames,
  sortLocations,
} from "./locations";

const MONITORING = {
  up: 1,
  down: 0,
  pending: 2,
  maintenance: 3,
};

const beat = (id: string, probeId: string, status = MONITORING.up, ping = 10): HeartbeatModel => ({
  id,
  monitor_id: "monitor-1",
  probe_id: probeId,
  status,
  ping,
  msg: "",
  time: "2026-10-24T00:00:00Z",
  end_time: "2026-10-24T00:00:00Z",
  duration: 0,
  down_count: 0,
  retries: 0,
  important: false,
  notified: false,
});

const location = (probeId: string, over: Partial<MonitorModelLocation> = {}): MonitorModelLocation => ({
  probe_id: probeId,
  probe_name: probeId,
  online: true,
  status: null,
  last_ping: null,
  last_heartbeat_at: null,
  ...over,
});

const probes = [
  { id: "local", name: "Local" },
  { id: "fra", name: "Frankfurt" },
];

describe("probeNames", () => {
  it("names each probe, the local one first and the others by name", () => {
    const more = [...probes, { id: "ams", name: "Amsterdam" }];
    // The API lists a monitor's probes by id, so the order it gives means nothing
    expect(probeNames(["ams", "fra", "local"], more, "Local")).toEqual(["Local", "Amsterdam", "Frankfurt"]);
    expect(probeNames(["fra", "ams"], more, "Local")).toEqual(["Amsterdam", "Frankfurt"]);
  });

  it("names the local probe even when the probes list is empty", () => {
    expect(probeNames(["local"], [], "Local")).toEqual(["Local"]);
  });

  it("puts a monitor with no probes on the local probe", () => {
    expect(probeNames(undefined, probes, "Local")).toEqual(["Local"]);
    expect(probeNames([], probes, "Local")).toEqual(["Local"]);
  });

  it("shows the id of a probe that is gone", () => {
    expect(probeNames(["local", "gone-probe"], probes, "Local")).toEqual(["Local", "gone-probe"]);
  });
});

describe("monitorProbes", () => {
  it("keeps the id of each probe, so that two probes with one name are told apart", () => {
    const same = [...probes, { id: "ams", name: "Frankfurt" }];
    expect(monitorProbes(["ams", "fra", "local"], same, "Local")).toEqual([
      { id: "local", name: "Local" },
      { id: "ams", name: "Frankfurt" },
      { id: "fra", name: "Frankfurt" },
    ]);
  });
});

describe("sortLocations", () => {
  it("puts the local probe first and the others by name, without changing the list it is given", () => {
    const locations = [
      location("z-id", { probe_name: "Amsterdam" }),
      location("local", { probe_name: "Local" }),
      location("a-id", { probe_name: "Frankfurt" }),
    ];
    expect(sortLocations(locations).map((l) => l.probe_id)).toEqual(["local", "z-id", "a-id"]);
    expect(locations.map((l) => l.probe_id)).toEqual(["z-id", "local", "a-id"]);
  });
});

describe("hasLocations", () => {
  it("is true only for a monitor on more than one probe", () => {
    expect(hasLocations([location("local")])).toBe(false);
    expect(hasLocations(undefined)).toBe(false);
    expect(hasLocations([location("local"), location("fra")])).toBe(true);
  });
});

describe("heartbeatsOfProbe", () => {
  it("keeps the probe's heartbeats in their order", () => {
    const beats = [beat("1", "local"), beat("2", "fra"), beat("3", "local")];
    expect(heartbeatsOfProbe(beats, "local").map((b) => b.id)).toEqual(["1", "3"]);
    expect(heartbeatsOfProbe(beats, "fra").map((b) => b.id)).toEqual(["2"]);
    expect(heartbeatsOfProbe(beats, "other")).toEqual([]);
  });
});

describe("mergeHeartbeats", () => {
  it("adds the live heartbeats after the fetched ones", () => {
    const merged = mergeHeartbeats([beat("1", "local")], [beat("2", "local")], 150);
    expect(merged.map((b) => b.id)).toEqual(["1", "2"]);
  });

  it("doesn't repeat a live heartbeat a later fetch already returned", () => {
    const merged = mergeHeartbeats([beat("1", "local"), beat("2", "local")], [beat("2", "local"), beat("3", "local")], 150);
    expect(merged.map((b) => b.id)).toEqual(["1", "2", "3"]);
  });

  it("keeps the newest heartbeats up to the limit", () => {
    const merged = mergeHeartbeats([beat("1", "local"), beat("2", "local")], [beat("3", "local")], 2);
    expect(merged.map((b) => b.id)).toEqual(["2", "3"]);
  });
});

describe("currentStatus", () => {
  it("is the status of the location's latest heartbeat", () => {
    const beats = [beat("1", "fra", MONITORING.up), beat("2", "fra", MONITORING.down)];
    expect(currentStatus(location("fra", { status: MONITORING.up }), beats)).toBe(MONITORING.down);
  });

  it("is the status the monitor reports until a heartbeat arrives", () => {
    expect(currentStatus(location("fra", { status: MONITORING.maintenance }), [])).toBe(MONITORING.maintenance);
    expect(currentStatus(location("fra"), [])).toBeUndefined();
  });
});

describe("monitorStatusOf", () => {
  const status = (probeId: string, value: number | undefined, online = true) => ({ probeId, online, status: value });

  it("is Down while any online location is Down", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.down)])).toBe(MONITORING.down);
  });

  it("is Maintenance while any location is, and none is Down", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.maintenance)])).toBe(
      MONITORING.maintenance
    );
  });

  it("is Up while any location is Up", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.pending)])).toBe(MONITORING.up);
  });

  it("doesn't count an offline location or one without a status", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.down, false)])).toBe(MONITORING.up);
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", undefined)])).toBe(MONITORING.up);
  });

  it("is Pending when no location is known", () => {
    expect(monitorStatusOf([status("a", undefined), status("b", MONITORING.up, false)])).toBe(MONITORING.pending);
  });
});
