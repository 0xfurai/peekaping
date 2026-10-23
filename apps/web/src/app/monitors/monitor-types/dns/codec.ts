import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { DnsFormValues } from "./schema";
import { formatHostPort, getNumber, getString, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface DnsConfig {
  host: string;
  resolver_server: string;
  port: number;
  resolve_type: string;
}

// Shows the record, then the record type and resolver, as "example.com (A @ 1.1.1.1:53)"
export const formatTarget = (config: MonitorConfig): MonitorTarget | null => {
  const host = getString(config, "host");
  if (!host) return null;

  const resolveType = getString(config, "resolve_type");
  const resolver = formatHostPort(getString(config, "resolver_server"), getNumber(config, "port"));
  const details = [resolveType, resolver && `@ ${resolver}`].filter(Boolean).join(" ");

  return { label: details ? `${host} (${details})` : host };
};

export const deserialize = (data: MonitorMonitorResponseDto): DnsFormValues => {
  let config: DnsConfig = {
    host: "example.com",
    resolver_server: "1.1.1.1",
    port: 53,
    resolve_type: "A",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        host: parsedConfig.host || "example.com",
        resolver_server: parsedConfig.resolver_server || "1.1.1.1",
        port: parsedConfig.port ?? 53,
        resolve_type: parsedConfig.resolve_type || "A",
      };
    } catch (error) {
      console.error("Failed to parse DNS monitor config:", error);
    }
  }

  return {
    type: "dns",
    ...deserializeMonitorBase(data),
    host: config.host,
    resolver_server: config.resolver_server,
    port: config.port,
    resolve_type: config.resolve_type as DnsFormValues["resolve_type"],
  };
};

export const serialize = (values: DnsFormValues): MonitorCreateUpdateDto => {
  const config: DnsConfig = {
    host: values.host,
    resolver_server: values.resolver_server,
    port: values.port,
    resolve_type: values.resolve_type,
  };

  return {
    type: "dns",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
