import type { LimitsReport } from "@/api/types.gen";
import type { WorkspaceRole } from "@/hooks/useCurrentAccess";

// A workspace's usage against its limits, as its Usage page shows it (PP-163), from
// GET /workspace/limits. The limits are the workspace's plan (GLOSSARY.md): its own, which an
// instance admin sets, or the install's defaults, or none. Nothing here bills or upgrades.

type LimitName = keyof LimitsReport["limits"];

// The limits that count what the workspace has, in the order the page shows them. "used" of
// probes_per_monitor is the most probes one monitor runs on, of owned_workspaces how many the
// caller owns.
export const COUNTED_LIMITS = [
  "monitors",
  "status_pages",
  "custom_domains",
  "members",
  "owned_workspaces",
  "probes_per_monitor",
] as const satisfies readonly LimitName[];

// The limits that are a value rather than a count, after the counted ones
export const VALUE_LIMITS = ["min_check_interval", "retention_days"] as const satisfies readonly LimitName[];

export type CountedLimit = (typeof COUNTED_LIMITS)[number];
export type ValueLimit = (typeof VALUE_LIMITS)[number];

// ok under 80% of the limit, near from 80%, reached at the limit and over it
export type UsageLevel = "ok" | "near" | "reached";

/** How close a count is to its limit: near from 80% of it, reached at it and over it. */
export const usageLevel = (used: number, max: number): UsageLevel => {
  if (used >= max) return "reached";
  if (used * 5 >= max * 4) return "near";
  return "ok";
};

export type UsageRow =
  | { name: CountedLimit; kind: "count"; used: number; max: number; level: UsageLevel; percent: number }
  | { name: ValueLimit; kind: "value"; value: number };

// The rows of the limits that are set. The shortest check interval is the one that applies, the
// stricter of the install's MIN_CHECK_INTERVAL and the limit; the retention is the longest period
// the workspace may keep.
export const usageRows = (report: LimitsReport): UsageRow[] => {
  const rows: UsageRow[] = [];
  for (const name of COUNTED_LIMITS) {
    const { max, used } = report.limits[name];
    if (max == null) continue;
    const percent = Math.min(100, Math.round((used / max) * 100));
    rows.push({ name, kind: "count", used, max, level: usageLevel(used, max), percent });
  }
  const interval = report.limits.min_check_interval;
  if (interval.max != null) rows.push({ name: "min_check_interval", kind: "value", value: interval.used });
  const retention = report.limits.retention_days;
  if (retention.max != null) rows.push({ name: "retention_days", kind: "value", value: retention.max });
  return rows;
};

/** Whether the workspace is held to any limit. */
export const hasLimits = (report: LimitsReport | undefined): boolean =>
  report !== undefined && Object.values(report.limits).some((use) => use.max != null);

/** Whether the Usage page and its entry in the settings are there: for an owner or admin of a workspace with limits. */
export const canSeeUsage = (role: WorkspaceRole | undefined, report: LimitsReport | undefined): boolean =>
  (role === "owner" || role === "admin") && hasLimits(report);

type Translate = (key: string, vars?: Record<string, unknown>) => string;

// Where the limits come from: the install's name for its defaults, or that an administrator set
// them, which is also what a workspace's own limits say. Null when there are none.
export const usageSource = (report: LimitsReport, t: Translate): string | null => {
  switch (report.source) {
    case "install":
      return report.installLimitsName || t("usage.set_by_admin");
    case "workspace":
      return t("usage.set_by_admin");
    default:
      return null;
  }
};
