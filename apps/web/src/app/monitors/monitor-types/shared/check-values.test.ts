import { describe, expect, it } from "vitest";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { monitorTypes, type MonitorFormValues } from "../registry";
import { checkMonitorValues } from "./check-values";
import { INTERVAL_TOO_SHORT } from "./schema";

const defaults = { ...monitorTypes[0].defaultValues, name: "Site", url: "https://example.com" } as MonitorFormValues;
const options = { fields: {}, shouldUseNativeValidation: false } as never;

const probe = (id: string, supportedTypes: string[]): ProbeProbeResponse =>
  ({ id, name: id, location: "", status: "online", supported_types: supportedTypes, disabled: false }) as ProbeProbeResponse;
const probes = [probe("local", []), probe("web-only", ["tcp"])];

const check = (values: MonitorFormValues, mayUseDefaultProbes: boolean, minCheckInterval = 20) =>
  checkMonitorValues(values, undefined, options, { probes, mayUseDefaultProbes, minCheckInterval });

describe("checkMonitorValues, with no probe ticked", () => {
  const none = { ...defaults, probe_ids: [] } as MonitorFormValues;

  it("accepts a new monitor of someone who isn't an instance admin, and keeps it without probes", async () => {
    const result = await check(none, true);
    expect(result.errors).toEqual({});
    expect((result.values as MonitorFormValues).probe_ids).toEqual([]);
  });

  it("refuses it otherwise: an instance admin's monitor or a saved one needs a probe", async () => {
    const result = await check(none, false);
    expect(result.errors).toHaveProperty("probe_ids");
  });

  it("still refuses a new monitor that is wrong elsewhere", async () => {
    const result = await check({ ...none, url: "not a url" } as MonitorFormValues, true);
    expect(result.errors).toHaveProperty("url");
  });
});

describe("checkMonitorValues, with probes ticked", () => {
  it("flags a probe that doesn't run the type", async () => {
    const result = await check({ ...defaults, probe_ids: ["web-only"] } as MonitorFormValues, true);
    expect(result.errors).toHaveProperty("probe_ids");
  });

  it("accepts a probe that does", async () => {
    const result = await check({ ...defaults, probe_ids: ["local"] } as MonitorFormValues, true);
    expect(result.errors).toEqual({});
  });
});

describe("checkMonitorValues, with the install's minimum check interval", () => {
  const at = (interval: number, retryInterval: number) =>
    ({ ...defaults, probe_ids: ["local"], interval, retry_interval: retryInterval }) as MonitorFormValues;

  it("flags an interval and a retry interval below the minimum, even when they are 20 or more", async () => {
    const result = await check(at(30, 40), false, 60);
    expect(result.errors).toHaveProperty("interval");
    expect(result.errors).toHaveProperty("retry_interval");
    expect((result.errors as Record<string, { message: string }>).interval.message).toBe(INTERVAL_TOO_SHORT);
  });

  it("names the minimum in place of the schema's own 20, for a value below 20 too", async () => {
    const result = await check(at(10, 10), false, 60);
    expect((result.errors as Record<string, { message: string }>).interval.message).toBe(INTERVAL_TOO_SHORT);
  });

  it("accepts the minimum itself", async () => {
    const result = await check(at(60, 60), false, 60);
    expect(result.errors).toEqual({});
  });

  it("accepts 20 when the minimum is 20", async () => {
    const result = await check(at(20, 20), false, 20);
    expect(result.errors).toEqual({});
  });

  it("flags a monitor on the default probes too, and keeps no probes when it passes", async () => {
    const none = { ...at(30, 30), probe_ids: [] } as MonitorFormValues;
    expect((await check(none, true, 60)).errors).toHaveProperty("interval");
    const passing = await check({ ...none, interval: 60, retry_interval: 60 } as MonitorFormValues, true, 60);
    expect(passing.errors).toEqual({});
    expect((passing.values as MonitorFormValues).probe_ids).toEqual([]);
  });

  it("holds a push monitor to it too", async () => {
    const push = { ...monitorTypes.find((definition) => definition.type === "push")!.defaultValues, name: "Cron", probe_ids: ["local"], interval: 30, retry_interval: 60 } as MonitorFormValues;
    expect((await check(push, false, 60)).errors).toHaveProperty("interval");
  });
});
