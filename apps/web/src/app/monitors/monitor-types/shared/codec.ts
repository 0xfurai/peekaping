import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import type { MonitorBaseValues } from "./schema";

// The probes a saved monitor runs on. One with none, such as an older response, is on the local probe.
export const monitorProbeIds = (data: { probe_ids?: string[] | null }): string[] =>
  data.probe_ids?.length ? data.probe_ids : [LOCAL_PROBE_ID];

// The form values of the fields every monitor type has, read from a saved monitor. A field the
// API left out gets the value a new monitor starts with.
export const deserializeMonitorBase = (data: MonitorMonitorResponseDto): MonitorBaseValues => ({
  name: data.name ?? "",
  interval: data.interval ?? 60,
  timeout: data.timeout ?? 16,
  max_retries: data.max_retries ?? 3,
  retry_interval: data.retry_interval ?? 60,
  resend_interval: data.resend_interval ?? 10,
  notification_ids: data.notification_ids ?? [],
  tag_ids: data.tag_ids ?? [],
  probe_ids: monitorProbeIds(data),
  location_rule: data.location_rule ?? "any",
  location_threshold: data.location_threshold ?? null,
});

// The request body fields every monitor type sends. The type's codec adds `type` and `config`.
export const serializeMonitorBase = (
  values: MonitorBaseValues
): Pick<
  MonitorCreateUpdateDto,
  | "name"
  | "interval"
  | "timeout"
  | "max_retries"
  | "retry_interval"
  | "resend_interval"
  | "notification_ids"
  | "tag_ids"
  | "probe_ids"
  | "location_rule"
  | "location_threshold"
> => ({
  name: values.name,
  interval: values.interval,
  timeout: values.timeout,
  max_retries: values.max_retries,
  retry_interval: values.retry_interval,
  resend_interval: values.resend_interval,
  notification_ids: values.notification_ids,
  tag_ids: values.tag_ids,
  probe_ids: values.probe_ids,
  location_rule: values.location_rule,
  // The threshold is only for "at least"; the form keeps one typed before a switch to another rule
  location_threshold: values.location_rule === "at_least" ? values.location_threshold : null,
});
