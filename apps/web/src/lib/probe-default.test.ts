import { describe, expect, it } from "vitest";
import { setProbeDefaultRequest } from "./probe-default";

describe("setProbeDefaultRequest", () => {
  it("sends only is_default, so the probe's other fields stay as they are", () => {
    expect(setProbeDefaultRequest("p1", true)).toEqual({
      path: { id: "p1" },
      body: { is_default: true },
    });
    expect(setProbeDefaultRequest("p1", false).body).toEqual({ is_default: false });
  });
});
