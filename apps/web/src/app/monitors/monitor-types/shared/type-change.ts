import type { MonitorFormValues } from "../registry";

// The values a new monitor starts from when its type changes: the new type's defaults, with the
// name typed so far, the probes chosen and the channels ticked (the default channels it started
// on, or the user's own) kept. The probe section flags a probe the new type doesn't run on.
export const valuesForNewType = (
  defaults: MonitorFormValues,
  current: Pick<MonitorFormValues, "name" | "probe_ids" | "notification_ids">
): MonitorFormValues => ({
  ...defaults,
  name: current.name || defaults.name,
  probe_ids: current.probe_ids,
  notification_ids: current.notification_ids,
});
