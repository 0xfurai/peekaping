import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { monitorTypes } from "./registry";
import { monitorTypeDisabledReason, replacementForDisabledType, selectableMonitorTypes } from "./disabled";

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
    expect(monitorTypeDisabledReason(i18n.t, "docker")).toBe("The Docker Container type is turned off on this install");
  });

  it("names a type the app doesn't know as the server gave it", () => {
    expect(monitorTypeDisabledReason(i18n.t, "carrier-pigeon")).toBe("The carrier-pigeon type is turned off on this install");
  });
});

// /me may load, or come back, after the create form opened on the first type: the form then moves
// off a type the install turned off, unless the user picked that type
describe("replacementForDisabledType", () => {
  const [first, second] = typesOf(monitorTypes);
  const opened = { type: first, setByForm: first, dirty: false };

  it("moves a new monitor that started on http, once /me turns http off, to the next type", () => {
    // the form opens before /me says anything is off
    expect(replacementForDisabledType(monitorTypes, { ...opened, disabled: [] })).toBeUndefined();
    // then /me turns http off
    const next = replacementForDisabledType(monitorTypes, { ...opened, disabled: [first] });
    expect(first).toBe("http");
    expect(next?.type).toBe(second);
  });

  // The type select resets the form, so a type picked there isn't dirty: it differs from the one
  // the form set
  it("leaves a type the user picked by hand, even once it is turned off", () => {
    const picked = { type: "tcp", setByForm: first, dirty: false };
    expect(replacementForDisabledType(monitorTypes, { ...picked, disabled: ["tcp"] })).toBeUndefined();
    expect(replacementForDisabledType(monitorTypes, { ...opened, dirty: true, disabled: [first] })).toBeUndefined();
  });

  it("leaves a type that is still on", () => {
    expect(replacementForDisabledType(monitorTypes, { ...opened, disabled: ["docker"] })).toBeUndefined();
  });

  it("stays when every type is off", () => {
    expect(replacementForDisabledType(monitorTypes, { ...opened, disabled: typesOf(monitorTypes) })).toBeUndefined();
  });
});
