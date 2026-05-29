import type { MonitorMonitorResponseDto } from "@/api";

type MonitorConfig = Record<string, unknown>;

const parseConfig = (config?: string): MonitorConfig => {
  if (!config) return {};

  try {
    const parsed = JSON.parse(config);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch (error) {
    console.error("Failed to parse monitor config:", error);
    return {};
  }
};

const getString = (config: MonitorConfig, key: string): string | null => {
  const value = config[key];
  return typeof value === "string" && value.trim() ? value : null;
};

const getNumber = (config: MonitorConfig, key: string): number | null => {
  const value = config[key];
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }

  return null;
};

const formatHostPort = (host: string | null, port: number | null): string | null => {
  if (!host) return null;
  return port ? `${host}:${port}` : host;
};

export const formatMonitorTarget = (
  monitor?: Pick<MonitorMonitorResponseDto, "type" | "config"> | null
): string | null => {
  if (!monitor?.type) return null;

  const config = parseConfig(monitor.config);

  switch (monitor.type) {
    case "http":
    case "http-keyword":
    case "http-json-query":
      return getString(config, "url");
    case "tcp":
      return formatHostPort(getString(config, "host"), getNumber(config, "port"));
    case "dns": {
      const host = getString(config, "host");
      if (!host) return null;

      const resolveType = getString(config, "resolve_type");
      const resolver = formatHostPort(
        getString(config, "resolver_server"),
        getNumber(config, "port")
      );

      if (resolveType && resolver) return `${host} (${resolveType} @ ${resolver})`;
      if (resolveType) return `${host} (${resolveType})`;
      if (resolver) return `${host} (@ ${resolver})`;

      return host;
    }
    case "ping":
      return getString(config, "host");
    case "grpc-keyword":
      return getString(config, "grpcUrl");
    case "docker":
      return getString(config, "container_id");
    default:
      return null;
  }
};
