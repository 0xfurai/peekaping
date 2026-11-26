import { z } from "zod";

// The longest data retention period the settings API takes, in days (100 years)
export const MAX_DATA_RETENTION_DAYS = 36500;

type Translate = (key: string, vars?: Record<string, unknown>) => string;

// GET /workspace/limits as far as the retention field reads it
type RetentionLimits = { data?: { limits: { retention_days: { max?: number | null } } } };

/**
 * The longest data retention period the workspace may keep: its retention_days limit (PP-161), or
 * null when it has none or its limits aren't loaded. A limit longer than the settings API takes is
 * that API's longest: the server refuses anything past it either way.
 */
export const retentionCap = (limits: RetentionLimits | undefined): number | null => {
  const max = limits?.data?.limits.retention_days.max ?? null;
  return max === null ? null : Math.min(max, MAX_DATA_RETENTION_DAYS);
};

/**
 * The retention field's schema: a whole number of days from 1, and no more than the workspace's
 * cap, which a longer period is refused with as the server words it (LIMIT_REACHED retention_days)
 */
export const dataRetentionSchema = (t: Translate, cap: number | null) => {
  const days = z.coerce
    .number()
    .int()
    .min(1, { message: t("settings.validation.min_days") });
  return z.object({
    value: cap === null ? days : days.max(cap, { message: t("limits.reached.retention_days", { max: cap }) }),
  });
};
