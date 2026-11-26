export type MonitorConfig = Record<string, unknown>;

// What a monitor checks, shown in its page header. href is set only for a
// target the user can open in the browser.
export interface MonitorTarget {
  label: string;
  href?: string;
}

export const parseMonitorConfig = (config?: string): MonitorConfig => {
  if (!config) return {};

  try {
    const parsed = JSON.parse(config);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch (error) {
    console.error("Failed to parse monitor config:", error);
    return {};
  }
};

export const getString = (config: MonitorConfig, key: string): string | null => {
  const value = config[key];
  return typeof value === "string" && value.trim() ? value : null;
};

export const getNumber = (config: MonitorConfig, key: string): number | null => {
  const value = config[key];
  return typeof value === "number" && Number.isFinite(value) ? value : null;
};

export const getStrings = (config: MonitorConfig, key: string): string[] => {
  const value = config[key];
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && !!item.trim());
};

// An IPv6 address gets brackets, so the port doesn't read as its last group.
// A host with a scheme, such as mqtt://broker, is left as it is.
export const formatHostPort = (host: string | null, port: number | null): string | null => {
  if (!host) return null;
  if (!port) return host;

  const isIPv6 = host.includes(":") && !host.includes("://") && !host.startsWith("[");
  return isIPv6 ? `[${host}]:${port}` : `${host}:${port}`;
};

// Joins a resource and the server it lives on, such as a topic and its broker
export const formatOnServer = (resource: string | null, server: string | null): string | null => {
  if (resource && server) return `${resource} @ ${server}`;
  return resource || server;
};

export const textTarget = (label: string | null): MonitorTarget | null =>
  label ? { label } : null;

// Links only http(s) URLs, so a stored javascript: URL never becomes an href
export const formatUrlTarget = (config: MonitorConfig): MonitorTarget | null => {
  const url = getString(config, "url");
  if (!url) return null;
  return /^https?:\/\//i.test(url) ? { label: url, href: url } : { label: url };
};
