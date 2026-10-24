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

  it("puts a live heartbeat that is older than the fetched ones in time order", () => {
    const at = (id: string, time: string) => ({ ...beat(id, "local"), time });
    const merged = mergeHeartbeats(
      [at("1", "2026-10-24T00:01:00Z"), at("3", "2026-10-24T00:03:00Z")],
      [at("2", "2026-10-24T00:02:00Z")],
      150
    );
    expect(merged.map((b) => b.id)).toEqual(["1", "2", "3"]);
  });

  it("keeps the newest by time, not by position, when over the limit", () => {
    const at = (id: string, time: string) => ({ ...beat(id, "local"), time });
    const merged = mergeHeartbeats(
      [at("1", "2026-10-24T00:01:00Z"), at("3", "2026-10-24T00:03:00Z")],
      [at("2", "2026-10-24T00:02:00Z")],
      2
    );
    expect(merged.map((b) => b.id)).toEqual(["2", "3"]);
  });

  it("keeps one of two heartbeats with the same id, in its place by time", () => {
    const at = (id: string, time: string) => ({ ...beat(id, "local"), time });
    const merged = mergeHeartbeats(
      [at("1", "2026-10-24T00:01:00Z"), at("2", "2026-10-24T00:02:00Z")],
      [at("2", "2026-10-24T00:09:00Z"), at("3", "2026-10-24T00:03:00Z")],
      150
    );
    expect(merged.map((b) => b.id)).toEqual(["1", "2", "3"]);
    expect(merged[1].time).toBe("2026-10-24T00:02:00Z");
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
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.down)], "any", null)).toBe(MONITORING.down);
  });

  it("is Maintenance while any location is, and none is Down", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.maintenance)], "any", null)).toBe(
      MONITORING.maintenance
    );
  });

  it("is Up while any location is Up", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.pending)], "any", null)).toBe(MONITORING.up);
  });

  it("doesn't count an offline location or one without a status", () => {
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", MONITORING.down, false)], "any", null)).toBe(MONITORING.up);
    expect(monitorStatusOf([status("a", MONITORING.up), status("b", undefined)], "any", null)).toBe(MONITORING.up);
  });

  it("is Pending when no location is known", () => {
    expect(monitorStatusOf([status("a", undefined), status("b", MONITORING.up, false)], "any", null)).toBe(MONITORING.pending);
  });

  describe("under the all rule", () => {
    it("is Down only when every location that counts is Down", () => {
      expect(monitorStatusOf([status("a", MONITORING.down), status("b", MONITORING.down)], "all", null)).toBe(
        MONITORING.down
      );
      expect(monitorStatusOf([status("a", MONITORING.down), status("b", MONITORING.up)], "all", null)).toBe(
        MONITORING.up
      );
    });

    it("doesn't count an offline location or one without a status", () => {
      expect(
        monitorStatusOf(
          [status("a", MONITORING.down), status("b", MONITORING.up, false), status("c", undefined)],
          "all",
          null
        )
      ).toBe(MONITORING.down);
    });

    it("is not Down when no location counts", () => {
      expect(monitorStatusOf([status("a", MONITORING.down, false), status("b", undefined)], "all", null)).toBe(
        MONITORING.pending
      );
    });

    it("is Maintenance when a location is in maintenance and not every one is Down", () => {
      expect(monitorStatusOf([status("a", MONITORING.down), status("b", MONITORING.maintenance)], "all", null)).toBe(
        MONITORING.maintenance
      );
    });
  });

  describe("under the at_least rule", () => {
    const threeLocations = (a: number, b: number, c: number) => [
      status("a", a),
      status("b", b),
      status("c", c),
    ];

    it("is Down when at least N locations are Down", () => {
      const { down, up } = MONITORING;
      expect(monitorStatusOf(threeLocations(down, up, up), "at_least", 2)).toBe(MONITORING.up);
      expect(monitorStatusOf(threeLocations(down, down, up), "at_least", 2)).toBe(MONITORING.down);
      expect(monitorStatusOf(threeLocations(down, down, down), "at_least", 2)).toBe(MONITORING.down);
    });

    it("doesn't count an offline Down location or one without a status", () => {
      const { down, up } = MONITORING;
      expect(
        monitorStatusOf([status("a", down), status("b", down, false), status("c", up)], "at_least", 2)
      ).toBe(MONITORING.up);
      expect(monitorStatusOf([status("a", down), status("b", undefined), status("c", up)], "at_least", 2)).toBe(
        MONITORING.up
      );
    });

    it("is never Down for a threshold more than can report, or none", () => {
      const { down } = MONITORING;
      expect(monitorStatusOf([status("a", down), status("b", down)], "at_least", 3)).toBe(MONITORING.pending);
      expect(monitorStatusOf([status("a", down)], "at_least", null)).toBe(MONITORING.pending);
    });
  });
});
