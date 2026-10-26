import { LOCATION_THRESHOLD_INVALID, LOCATION_THRESHOLD_TOO_HIGH, type LocationRule } from "./schema";
import { probeIdsOf } from "./probe-support";

// The message for an "at least" rule with no threshold, one under 1 or one higher than the probes
// the monitor runs on, if it is one. Other rules don't use the threshold, so a leftover one isn't
// checked. The type's schema can't tell, since it needs the probes and the rule beside
// the threshold. A push monitor is fixed to the local probe and the any rule, whatever the form
// holds, so there is nothing to check.
export const locationThresholdError = (values: {
  type: string;
  probe_ids: string[];
  location_rule: LocationRule;
  location_threshold: number | null;
}): string | undefined => {
  if (values.type === "push" || values.location_rule !== "at_least") return undefined;
  if (values.location_threshold === null || !Number.isInteger(values.location_threshold) || values.location_threshold < 1) {
    return LOCATION_THRESHOLD_INVALID;
  }
  return values.location_threshold > probeIdsOf(values.type, values.probe_ids).length
    ? LOCATION_THRESHOLD_TOO_HIGH
    : undefined;
};
