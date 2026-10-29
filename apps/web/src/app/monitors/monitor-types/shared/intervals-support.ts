import type { FieldErrors } from "react-hook-form";
import { INTERVAL_TOO_SHORT, TIMEOUT_TOO_LONG } from "./schema";

// The message for a timeout above 80% of the check interval, if it is one. The type's schema can't
// tell, since it needs the interval beside the timeout. Compared in whole seconds, as the API does.
export const timeoutError = (values: { interval: number; timeout: number }): string | undefined =>
  values.timeout * 5 > values.interval * 4 ? TIMEOUT_TOO_LONG : undefined;

// The form with its interval and retry interval raised to the install's minimum, for the values a
// new monitor starts with. A value already above it stays.
export const withMinimumIntervals = <T extends { interval: number; retry_interval: number }>(
  values: T,
  minCheckInterval: number
): T => ({
  ...values,
  interval: Math.max(values.interval, minCheckInterval),
  retry_interval: Math.max(values.retry_interval, minCheckInterval),
});

// The errors for an interval or retry interval below the install's minimum, by field. The form holds
// what the inputs gave it, so the values are read as numbers, as the schema's coercion does.
export const intervalMinimumErrors = (
  values: { interval: unknown; retry_interval: unknown },
  minCheckInterval: number
): FieldErrors<{ interval: number; retry_interval: number }> => {
  const errors: FieldErrors<{ interval: number; retry_interval: number }> = {};
  if (Number(values.interval) < minCheckInterval) errors.interval = { type: "min", message: INTERVAL_TOO_SHORT };
  if (Number(values.retry_interval) < minCheckInterval) errors.retry_interval = { type: "min", message: INTERVAL_TOO_SHORT };
  return errors;
};
