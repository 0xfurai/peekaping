import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { PingFormValues } from "./schema";
import { getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface PingConfig {
  host: string;
  packet_size: number;
}

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(getString(config, "host"));

export const deserialize = (data: MonitorMonitorResponseDto): PingFormValues => {
  let config: PingConfig = {
    host: "example.com",
    packet_size: 32,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        host: parsedConfig.host || "example.com",
        packet_size: parsedConfig.packet_size ?? 32,
      };
    } catch (error) {
      console.error("Failed to parse ping monitor config:", error);
    }
  }

  return {
    type: "ping",
    ...deserializeMonitorBase(data),
    host: config.host,
    packet_size: config.packet_size,
  };
};

export const serialize = (values: PingFormValues): MonitorCreateUpdateDto => {
  const config: PingConfig = {
    host: values.host,
    packet_size: values.packet_size ?? 32,
  };

  return {
    type: "ping",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
