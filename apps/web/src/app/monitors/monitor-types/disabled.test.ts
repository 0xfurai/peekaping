import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { monitorTypes } from "./registry";
import { monitorTypeDisabledReason, selectableMonitorTypes } from "./disabled";

const typesOf = (definitions: readonly { type: string }[]) => definitions.map((definition) => definition.type);

describe("selectableMonitorTypes", () => {
  it("offers every type when the install turns none off", () => {
    expect(typesOf(selectableMonitorTypes(monitorTypes, { disabled: [] }))).toEqual(typesOf(monitorTypes));
  });

  it("leaves out the types the install turns off, and keeps the order of the rest", () => {
    const offered = typesOf(selectableMonitorTypes(monitorTypes, { disabled: ["docker", "snmp"] }));
    expect(offered).not.toContain("docker");
    expect(offered).not.toContain("snmp");
    expect(offered).toEqual(typesOf(monitorTypes).filter((type) => type !== "docker" && type !== "snmp"));
  });

  it("keeps the type a saved monitor has, but still hides the other turned-off types", () => {
    const offered = typesOf(selectableMonitorTypes(monitorTypes, { disabled: ["docker", "snmp"], keep: "docker" }));
    expect(offered).toContain("docker");
    expect(offered).not.toContain("snmp");
  });

  it("ignores a turned-off type the app doesn't know", () => {
    expect(typesOf(selectableMonitorTypes(monitorTypes, { disabled: ["carrier-pigeon"] }))).toEqual(typesOf(monitorTypes));
  });
});

describe("monitorTypeDisabledReason", () => {
  it("names the type by its label in the Type select", () => {
    expect(monitorTypeDisabledReason(i18n.t, "docker")).toBe("Docker Container monitors are turned off on this install");
  });

  it("names a type the app doesn't know as the server gave it", () => {
    expect(monitorTypeDisabledReason(i18n.t, "carrier-pigeon")).toBe("carrier-pigeon monitors are turned off on this install");
  });
});
