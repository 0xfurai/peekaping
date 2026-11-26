import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { dataRetentionSchema, MAX_DATA_RETENTION_DAYS, retentionCap } from "./data-retention";

const t = i18n.t.bind(i18n) as unknown as (key: string, vars?: Record<string, unknown>) => string;

const refusal = (schema: ReturnType<typeof dataRetentionSchema>, value: unknown) => {
  const parsed = schema.safeParse({ value });
  return parsed.success ? undefined : parsed.error.issues[0].message;
};

// GET /workspace/limits as the page reads it, with the workspace's retention_days limit
const limitsWith = (max: number | null) => ({
  data: { limits: { retention_days: { max, used: 30, source: max === null ? "none" : "install" } } },
});

describe("dataRetentionSchema", () => {
  it("takes a period up to the workspace's cap, and refuses a longer one with the limit's message", () => {
    const schema = dataRetentionSchema(t, 30);
    expect(refusal(schema, "30")).toBeUndefined();
    expect(refusal(schema, "1")).toBeUndefined();
    expect(refusal(schema, "31")).toBe("This workspace can keep data for at most 30 days");
  });

  it("takes any period up to 100 years without a cap, as before", () => {
    const schema = dataRetentionSchema(t, null);
    expect(refusal(schema, String(MAX_DATA_RETENTION_DAYS))).toBeUndefined();
    expect(refusal(schema, "0")).toBe("Must be at least 1 day");
  });
});

describe("retentionCap", () => {
  it("is the workspace's retention_days limit, or none", () => {
    expect(retentionCap(limitsWith(30))).toBe(30);
    expect(retentionCap(limitsWith(null))).toBeNull();
    expect(retentionCap(undefined)).toBeNull();
  });

  it("is never longer than the settings API takes", () => {
    expect(retentionCap(limitsWith(MAX_DATA_RETENTION_DAYS + 1))).toBe(MAX_DATA_RETENTION_DAYS);
    expect(retentionCap(limitsWith(100000))).toBe(MAX_DATA_RETENTION_DAYS);
    expect(retentionCap(limitsWith(MAX_DATA_RETENTION_DAYS))).toBe(MAX_DATA_RETENTION_DAYS);
  });
});
