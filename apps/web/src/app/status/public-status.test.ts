import { describe, expect, it } from "vitest";
import type { StatusPagePublicHeartbeatDto } from "@/api/types.gen";
import { hasPublicLocations, publicMonitorStatus } from "./public-status";

const MONITORING = { down: 0, up: 1, pending: 2, maintenance: 3 };

const beat = (status: number): StatusPagePublicHeartbeatDto => ({
  id: `beat-${status}`,
  status,
  ping: 10,
  time: "2026-10-25T00:00:00Z",
  end_time: "2026-10-25T00:00:00Z",
});

describe("publicMonitorStatus", () => {
  it("is the monitor's own status, whatever its latest heartbeat says", () => {
    expect(publicMonitorStatus({ status: MONITORING.down, heartbeats: [beat(MONITORING.up)] })).toBe(
      MONITORING.down
    );
    expect(publicMonitorStatus({ status: MONITORING.up, heartbeats: [beat(MONITORING.down)] })).toBe(
      MONITORING.up
    );
  });

  it("keeps a status of Down, which is 0", () => {
    expect(publicMonitorStatus({ status: 0, heartbeats: [beat(MONITORING.up)] })).toBe(0);
  });

  it("falls back to the latest heartbeat when the response has no status", () => {
    expect(publicMonitorStatus({ heartbeats: [beat(MONITORING.up), beat(MONITORING.maintenance)] })).toBe(
      MONITORING.maintenance
    );
  });

  it("is unknown for a monitor with no status and no heartbeats", () => {
    expect(publicMonitorStatus({ heartbeats: [] })).toBeUndefined();
    expect(publicMonitorStatus({ heartbeats: null })).toBeUndefined();
  });
});

describe("hasPublicLocations", () => {
  const location = { name: "Local", status: 1, uptime_24h: 100, heartbeats: [] };

  it("is true for a monitor with locations", () => {
    expect(hasPublicLocations({ locations: [location, { ...location, name: "Paris" }] })).toBe(true);
  });

  it("is false without locations", () => {
    expect(hasPublicLocations({})).toBe(false);
    expect(hasPublicLocations({ locations: null })).toBe(false);
    expect(hasPublicLocations({ locations: [] })).toBe(false);
  });
});
