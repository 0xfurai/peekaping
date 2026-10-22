import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { PushFormValues } from "./schema";
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
    config: JSON.stringify(config),
    push_token: values.pushToken,
  };
};
