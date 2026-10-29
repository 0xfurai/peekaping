import { describe, expect, it } from "vitest";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { runFromChoices, runFromDefaultIds, toggleRunFrom } from "./run-from";

const probe = (id: string, over: Partial<ProbeProbeResponse> = {}): ProbeProbeResponse => ({
  id,
  name: id,
  location: "",
  status: "online",
  supported_types: [],
  disabled: false,
  instances: [],
  assign_all_pending: false,
  is_default: false,
  notification_ids: [],
  token_prefix: "",
  created_at: "2026-10-23T00:00:00Z",
  updated_at: "2026-10-23T00:00:00Z",
  ...over,
});

const local = probe("local", { name: "Local", location: "Frankfurt", is_default: true });
const eu = probe("eu", { name: "Berlin", location: "", supported_types: ["http", "tcp"] });
const us = probe("us", { name: "Virginia", location: "US East", supported_types: ["http"], is_default: true });
const off = probe("off", { name: "Paused", location: "X", disabled: true });

describe("runFromChoices", () => {
  it("names each probe with its location, and leaves out an empty location", () => {
    const { choices } = runFromChoices([local, eu], "http", ["local"]);
    expect(choices.map((c) => c.label)).toEqual(["Local · Frankfurt", "Berlin"]);
  });

  it("ticks the ids it is given", () => {
    const { choices } = runFromChoices([local, eu], "http", ["eu"]);
    expect(choices.map((c) => [c.id, c.ticked])).toEqual([
      ["local", false],
      ["eu", true],
    ]);
  });

  it("lists only enabled probes that run the type", () => {
    const { choices } = runFromChoices([local, eu, us, off], "tcp", []);
    expect(choices.map((c) => c.id)).toEqual(["local", "eu"]);
  });

  it("is hidden with one probe to choose from, and shown with two", () => {
    expect(runFromChoices([local, eu], "docker", []).hidden).toBe(true);
    expect(runFromChoices([local, eu], "http", []).hidden).toBe(false);
    expect(runFromChoices([local], "http", []).hidden).toBe(true);
  });

  it("is hidden for a push monitor, which runs on the local probe", () => {
    expect(runFromChoices([local, eu], "push", []).hidden).toBe(true);
  });

  // A saved monitor whose probe was disabled, or no longer runs the type, can move to the one
  // probe left only through the list
  it("is shown with one probe to choose from when the monitor runs on a probe that isn't one", () => {
    expect(runFromChoices([off, eu], "http", ["off"]).hidden).toBe(false);
    expect(runFromChoices([off, eu], "http", ["eu", "off"]).hidden).toBe(false);
    expect(runFromChoices([local, eu], "docker", ["eu"]).hidden).toBe(false);
  });

  it("stays hidden with one probe to choose from that the monitor already runs on", () => {
    expect(runFromChoices([off, eu], "http", ["eu"]).hidden).toBe(true);
  });

  it("stays hidden with no probe to choose from", () => {
    expect(runFromChoices([], "http", ["local"]).hidden).toBe(true);
    expect(runFromChoices([off], "http", ["off"]).hidden).toBe(true);
  });
});

describe("runFromDefaultIds", () => {
  it("is the default probes that run the type", () => {
    expect(runFromDefaultIds([local, eu, us], "http")).toEqual(["local", "us"]);
    expect(runFromDefaultIds([local, eu, us], "tcp")).toEqual(["local"]);
  });

  it("leaves out a disabled default probe", () => {
    expect(runFromDefaultIds([{ ...local, disabled: true }, eu, us], "http")).toEqual(["us"]);
  });

  it("is the one probe there is when the list is hidden, default or not", () => {
    expect(runFromDefaultIds([local, eu], "docker")).toEqual(["local"]);
    expect(runFromDefaultIds([eu], "http")).toEqual(["eu"]);
  });

  it("is the local probe before the probes load", () => {
    expect(runFromDefaultIds([], "http")).toEqual(["local"]);
  });
});

describe("toggleRunFrom", () => {
  it("adds and removes a probe, keeping the ones the list doesn't show", () => {
    expect(toggleRunFrom(["gone", "local"], "eu", true)).toEqual(["gone", "local", "eu"]);
    expect(toggleRunFrom(["gone", "local", "eu"], "local", false)).toEqual(["gone", "eu"]);
  });

  it("adds a probe once", () => {
    expect(toggleRunFrom(["eu"], "eu", true)).toEqual(["eu"]);
  });
});
