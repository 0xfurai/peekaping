import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { PushFormValues } from "./schema";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

export interface PushConfig {
  pushToken: string;
}

export const deserialize = (data: MonitorMonitorResponseDto): PushFormValues => {
  let config: Partial<PushConfig> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse push monitor config:", error);
    config = {};
  }

  return {
    type: "push",
    ...deserializeMonitorBase(data),
    pushToken: data.push_token || config.pushToken || "", // Get from API or config
  };
};

export const serialize = (values: PushFormValues): MonitorCreateUpdateDto => {
  const config: PushConfig = {
    pushToken: values.pushToken,
  };

  return {
    type: "push",
    ...serializeMonitorBase(values),
    // The monitored service pushes to this process, so the monitor runs on the local probe
    // whichever probe the form holds (the probe section shows it fixed to Local)
    probe_ids: [LOCAL_PROBE_ID],
    // One location, so no rule to choose
    location_rule: "any",
    location_threshold: null,
    config: JSON.stringify(config),
    push_token: values.pushToken,
  };
};
