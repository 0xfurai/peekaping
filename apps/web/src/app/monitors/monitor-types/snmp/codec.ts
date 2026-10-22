import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { SnmpFormValues } from "./schema";
import { formatHostPort, getNumber, getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface SnmpConfig {
  host: string;
  port: number;
  community: string;
  snmp_version: string;
  oid: string;
  json_path?: string;
  json_path_operator?: string;
  expected_value?: string;
}

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(formatHostPort(getString(config, "host"), getNumber(config, "port")));

export const deserialize = (data: MonitorMonitorResponseDto): SnmpFormValues => {
  let config: SnmpConfig = {
    host: "127.0.0.1",
    port: 161,
    community: "public",
    snmp_version: "v2c",
    oid: "1.3.6.1.2.1.1.1.0",
    json_path: "$",
    json_path_operator: "eq",
    expected_value: "",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        host: parsedConfig.host || "127.0.0.1",
        port: parsedConfig.port ?? 161,
        community: parsedConfig.community || "public",
        snmp_version: parsedConfig.snmp_version || "v2c",
        oid: parsedConfig.oid || "1.3.6.1.2.1.1.1.0",
        json_path: parsedConfig.json_path || "$",
        json_path_operator: parsedConfig.json_path_operator || "eq",
        expected_value: parsedConfig.expected_value || "",
      };
    } catch (error) {
      console.error("Failed to parse SNMP monitor config:", error);
    }
  }

  return {
    type: "snmp",
    ...deserializeMonitorBase(data),
    host: config.host,
    port: config.port,
    community: config.community,
    snmp_version: config.snmp_version as "v1" | "v2c" | "v3",
    oid: config.oid,
    json_path: config.json_path,
    json_path_operator: config.json_path_operator as
      | "eq"
      | "ne"
      | "lt"
      | "gt"
      | "le"
      | "ge"
      | undefined,
    expected_value: config.expected_value,
  };
};

export const serialize = (values: SnmpFormValues): MonitorCreateUpdateDto => {
  const config: SnmpConfig = {
    host: values.host,
    port: values.port ?? 161,
    community: values.community,
    snmp_version: values.snmp_version,
    oid: values.oid,
    json_path: values.json_path || "$",
    json_path_operator: values.json_path_operator || "eq",
    expected_value: values.expected_value || "",
  };

  return {
    type: "snmp",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
