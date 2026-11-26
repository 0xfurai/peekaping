import { describe, expect, it } from "vitest";
import { getMonitorType, type MonitorFormValues } from "../registry";
import { valuesForNewType } from "./type-change";

describe("valuesForNewType", () => {
  const http = getMonitorType("http")!.defaultValues as MonitorFormValues;
  const tcp = getMonitorType("tcp")!.defaultValues as MonitorFormValues;
  const entered = { ...http, name: "Site", probe_ids: ["eu"], notification_ids: ["alerts", "team"] } as MonitorFormValues;

  it("starts from the new type's defaults", () => {
    const values = valuesForNewType(tcp, entered);
    expect(values.type).toBe("tcp");
    expect(values).toMatchObject({ ...tcp, name: "Site", probe_ids: ["eu"], notification_ids: ["alerts", "team"] });
  });

  // The default channels a new monitor starts on, or the ones the user ticked, aren't lost
  it("keeps the name, the probes and the channels", () => {
    const values = valuesForNewType(tcp, entered);
    expect(values.name).toBe("Site");
    expect(values.probe_ids).toEqual(["eu"]);
    expect(values.notification_ids).toEqual(["alerts", "team"]);
  });

  it("takes the new type's name when none was typed", () => {
    expect(valuesForNewType(tcp, { ...entered, name: "" }).name).toBe(tcp.name);
  });
});
