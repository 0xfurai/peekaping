import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { RedisFormValues } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface RedisConfig {
  databaseConnectionString: string;
  ignoreTls: boolean;
  caCert?: string;
  clientCert?: string;
  clientKey?: string;
}

export const serialize = (values: RedisFormValues): MonitorCreateUpdateDto => {
  const config: RedisConfig = {
    databaseConnectionString: values.databaseConnectionString,
    ignoreTls: values.ignoreTls,
    ...(values.caCert && { caCert: values.caCert }),
    ...(values.clientCert && { clientCert: values.clientCert }),
    ...(values.clientKey && { clientKey: values.clientKey }),
  };

  return {
    type: "redis",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};

export const deserialize = (data: MonitorMonitorResponseDto): RedisFormValues => {
  const config: RedisConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "redis",
    ...deserializeMonitorBase(data),
    databaseConnectionString:
      config.databaseConnectionString ?? "redis://user:password@host:port",
    ignoreTls: config.ignoreTls ?? false,
    caCert: config.caCert ?? "",
    clientCert: config.clientCert ?? "",
    clientKey: config.clientKey ?? "",
  };
};
