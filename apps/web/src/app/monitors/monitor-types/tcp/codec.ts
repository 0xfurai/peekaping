import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { TcpFormValues } from "./schema";
import { formatHostPort, getNumber, getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface TcpConfig {
  host: string;
  port: number;
}

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(formatHostPort(getString(config, "host"), getNumber(config, "port")));

export const deserialize = (data: MonitorMonitorResponseDto): TcpFormValues => {
  let config: TcpConfig = {
    host: "example.com",
    port: 80,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        host: parsedConfig.host || "example.com",
        port: parsedConfig.port ?? 80,
      };
    } catch (error) {
      console.error("Failed to parse TCP monitor config:", error);
    }
  }

  return {
    type: "tcp",
    ...deserializeMonitorBase(data),
    host: config.host,
    port: config.port,
  };
};

export const serialize = (values: TcpFormValues): MonitorCreateUpdateDto => {
  const config: TcpConfig = {
    host: values.host,
    port: values.port,
  };

  return {
    type: "tcp",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
