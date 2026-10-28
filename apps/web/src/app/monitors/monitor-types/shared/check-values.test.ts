import { describe, expect, it } from "vitest";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { monitorTypes, type MonitorFormValues } from "../registry";
import { checkMonitorValues } from "./check-values";

const defaults = { ...monitorTypes[0].defaultValues, name: "Site", url: "https://example.com" } as MonitorFormValues;
const options = { fields: {}, shouldUseNativeValidation: false } as never;

const probe = (id: string, supportedTypes: string[]): ProbeProbeResponse =>
  ({ id, name: id, location: "", status: "online", supported_types: supportedTypes, disabled: false }) as ProbeProbeResponse;
const probes = [probe("local", []), probe("web-only", ["tcp"])];

const check = (values: MonitorFormValues, mayUseDefaultProbes: boolean) =>
  checkMonitorValues(values, undefined, options, { probes, mayUseDefaultProbes });

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
