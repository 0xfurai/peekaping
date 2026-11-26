import { describe, expect, it } from "vitest";
import type { StatusPagePublicHeartbeatDto } from "@/api/types.gen";
import {
  NOT_MONITORED,
  hasPublicLocations,
  isPublicMonitorChecked,
  publicMonitorStatus,
  statusShownFor,
} from "./public-status";

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

describe("isPublicMonitorChecked", () => {
  it("is true for an active monitor", () => {
    expect(isPublicMonitorChecked({ active: true })).toBe(true);
  });

  // The server sends active: false for any monitor that isn't checked, paused by hand or of a turned-off type
  it("is false for a monitor that isn't active", () => {
    expect(isPublicMonitorChecked({ active: false })).toBe(false);
  });

  it("is true for a monitor with no active field", () => {
    expect(isPublicMonitorChecked({} as { active: boolean })).toBe(true);
  });
});

describe("statusShownFor", () => {
  it("is the status itself for a monitor that is checked", () => {
    expect(statusShownFor({ active: true }, MONITORING.down)).toBe(MONITORING.down);
    expect(statusShownFor({ active: true }, undefined)).toBeUndefined();
  });

  it("is Not monitored, whatever the last status was, for one that isn't", () => {
    expect(statusShownFor({ active: false }, MONITORING.down)).toBe(NOT_MONITORED);
    expect(statusShownFor({ active: false }, MONITORING.maintenance)).toBe(NOT_MONITORED);
    expect(statusShownFor({ active: false }, undefined)).toBe(NOT_MONITORED);
  });

  it("is a status no real monitor has", () => {
    expect(Object.values(MONITORING)).not.toContain(NOT_MONITORED);
  });
});
