import type { SlowResponseMode } from "./schema";

export const SLOW_RESPONSE_THRESHOLD_INVALID =
  "monitors.form.shared.slow_response.threshold_invalid";
export const SLOW_RESPONSE_MULTIPLIER_INVALID =
  "monitors.form.shared.slow_response.multiplier_invalid";
export const SLOW_RESPONSE_MIN_MS_INVALID =
  "monitors.form.shared.slow_response.min_ms_invalid";
export const SLOW_RESPONSE_CONSECUTIVE_INVALID =
  "monitors.form.shared.slow_response.consecutive_invalid";

export interface SlowResponseError {
  path:
    | "slow_response_threshold_ms"
    | "slow_response_multiplier"
    | "slow_response_min_ms"
    | "slow_response_consecutive";
  message: string;
}

export const slowResponseErrors = (values: {
  slow_response_mode: SlowResponseMode;
  slow_response_threshold_ms: number | null;
  slow_response_multiplier: number | null;
  slow_response_min_ms: number | null;
  slow_response_consecutive: number;
}): SlowResponseError[] => {
  if (values.slow_response_mode === "off") return [];

  const issues: SlowResponseError[] = [];

  if (
    !Number.isInteger(values.slow_response_consecutive) ||
    values.slow_response_consecutive < 1
  ) {
    issues.push({
      path: "slow_response_consecutive",
      message: SLOW_RESPONSE_CONSECUTIVE_INVALID,
    });
  }

  if (values.slow_response_mode === "fixed") {
    const threshold = values.slow_response_threshold_ms;
    if (threshold === null || !Number.isInteger(threshold) || threshold < 1) {
      issues.push({
        path: "slow_response_threshold_ms",
        message: SLOW_RESPONSE_THRESHOLD_INVALID,
      });
    }
  }

  if (values.slow_response_mode === "baseline") {
    const multiplier = values.slow_response_multiplier;
    if (
      multiplier === null ||
      !Number.isFinite(multiplier) ||
      multiplier <= 1
    ) {
      issues.push({
        path: "slow_response_multiplier",
        message: SLOW_RESPONSE_MULTIPLIER_INVALID,
      });
    }
    const minMs = values.slow_response_min_ms;
    if (minMs !== null && (!Number.isInteger(minMs) || minMs < 0)) {
      issues.push({
        path: "slow_response_min_ms",
        message: SLOW_RESPONSE_MIN_MS_INVALID,
      });
    }
  }

  return issues;
};
