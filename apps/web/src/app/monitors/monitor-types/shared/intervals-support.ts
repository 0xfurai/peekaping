import { TIMEOUT_TOO_LONG } from "./schema";

// The message for a timeout above 80% of the check interval, if it is one. The type's schema can't
// tell, since it needs the interval beside the timeout. Compared in whole seconds, as the API does.
export const timeoutError = (values: { interval: number; timeout: number }): string | undefined =>
  values.timeout * 5 > values.interval * 4 ? TIMEOUT_TOO_LONG : undefined;
