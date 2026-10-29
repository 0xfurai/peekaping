import { describe, expect, it } from "vitest";
import type { LimitsReport } from "@/api/types.gen";
import i18n from "@/i18n";
import { canSeeUsage, hasLimits, usageLevel, usageRows, usageSource } from "./workspace-usage";

type Use = LimitsReport["limits"]["monitors"];
const none: Use = { max: null, used: 0, source: "none" };

// GET /workspace/limits with the limits given, and none for the others
const report = (
  limits: Partial<LimitsReport["limits"]>,
  source: LimitsReport["source"] = "install",
  installLimitsName: string | null = null
): LimitsReport => ({
  source,
  installLimitsName,
  limits: {
    monitors: none,
    min_check_interval: { max: null, used: 20, source: "none" },
    status_pages: none,
    custom_domains: none,
    members: none,
    owned_workspaces: none,
    probes_per_monitor: none,
    retention_days: { max: null, used: 365, source: "none" },
    ...limits,
  },
});

const t = i18n.t.bind(i18n) as unknown as (key: string, vars?: Record<string, unknown>) => string;

describe("usageLevel", () => {
  it("is fine under 80% of the limit", () => {
    expect(usageLevel(15, 20)).toBe("ok");
    expect(usageLevel(0, 1)).toBe("ok");
  });

  it("warns at 80% of the limit and above", () => {
    expect(usageLevel(16, 20)).toBe("near");
    expect(usageLevel(19, 20)).toBe("near");
    expect(usageLevel(4, 5)).toBe("near");
  });

  it("is reached at the limit, and over it", () => {
    expect(usageLevel(20, 20)).toBe("reached");
    expect(usageLevel(1, 1)).toBe("reached");
    expect(usageLevel(25, 20)).toBe("reached");
  });
});

describe("usageRows", () => {
  it("has a row for each limit that is set, in order, and none for a limit that isn't", () => {
    const rows = usageRows(
      report({
        members: { max: 5, used: 4, source: "install" },
        monitors: { max: 20, used: 14, source: "install" },
      })
    );
    expect(rows.map((r) => r.name)).toEqual(["monitors", "members"]);
    expect(rows[0]).toEqual({ name: "monitors", kind: "count", used: 14, max: 20, level: "ok", percent: 70 });
    expect(rows[1]).toEqual({ name: "members", kind: "count", used: 4, max: 5, level: "near", percent: 80 });
  });

  it("fills the bar at most to the end when a workspace is over its limit", () => {
    const [row] = usageRows(report({ status_pages: { max: 2, used: 3, source: "workspace" } }, "workspace"));
    expect(row).toMatchObject({ level: "reached", percent: 100 });
  });

  it("shows the minimum interval and the retention as values, not as usage", () => {
    const rows = usageRows(
      report({
        min_check_interval: { max: 60, used: 60, source: "install" },
        retention_days: { max: 30, used: 365, source: "install" },
      })
    );
    expect(rows).toEqual([
      { name: "min_check_interval", kind: "value", value: 60 },
      { name: "retention_days", kind: "value", value: 30 },
    ]);
  });

  it("shows the interval that applies, the stricter of the install's minimum and the limit", () => {
    const [row] = usageRows(report({ min_check_interval: { max: 30, used: 60, source: "install" } }));
    expect(row).toEqual({ name: "min_check_interval", kind: "value", value: 60 });
  });
});

describe("hasLimits", () => {
  it("is false without a report, and for a workspace with no limit set", () => {
    expect(hasLimits(undefined)).toBe(false);
    expect(hasLimits(report({}, "none"))).toBe(false);
  });

  it("is true when any limit is set", () => {
    expect(hasLimits(report({ owned_workspaces: { max: 3, used: 1, source: "install" } }))).toBe(true);
    expect(hasLimits(report({ retention_days: { max: 30, used: 30, source: "install" } }))).toBe(true);
  });
});

describe("canSeeUsage", () => {
  const limited = report({ monitors: { max: 20, used: 14, source: "install" } });

  it("lets an owner and an admin see the usage of a workspace with limits", () => {
    expect(canSeeUsage("owner", limited)).toBe(true);
    expect(canSeeUsage("admin", limited)).toBe(true);
  });

  it("hides it from a plain member, and while the role isn't known", () => {
    expect(canSeeUsage("member", limited)).toBe(false);
    expect(canSeeUsage(undefined, limited)).toBe(false);
  });

  it("hides it from everyone when the workspace has no limits", () => {
    expect(canSeeUsage("owner", report({}, "none"))).toBe(false);
    expect(canSeeUsage("owner", undefined)).toBe(false);
  });
});

describe("usageSource", () => {
  const monitors = { monitors: { max: 20, used: 14, source: "install" as const } };

  it("names the install's defaults by the install's name for them", () => {
    expect(usageSource(report(monitors, "install", "Free tier"), t)).toBe("Free tier");
  });

  it("says the administrator set the install's defaults when the install has no name for them", () => {
    expect(usageSource(report(monitors, "install", null), t)).toBe("Set by your administrator");
  });

  it("says the administrator set the workspace's own limits, whatever the install calls its defaults", () => {
    expect(usageSource(report(monitors, "workspace", "Free tier"), t)).toBe("Set by your administrator");
  });
});
