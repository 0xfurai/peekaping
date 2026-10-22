import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { RabbitMQFormValues } from "./schema";
import { getStrings, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface RabbitMQConfig {
  nodes: string[]; // Server expects array of strings
  username: string;
  password: string;
}

export const serialize = (values: RabbitMQFormValues): MonitorCreateUpdateDto => {
  const config: RabbitMQConfig = {
    nodes: values.nodes.map(node => node.url), // Convert [{ url: "" }] to string[]
    username: values.username,
    password: values.password,
  };

  return {
    type: values.type,
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};

// Only the host and port, so credentials in a node URL stay out of the header.
// A node the browser can't parse, such as one with port 99999 that the server
// accepts, is left out rather than shown whole.
const formatNode = (node: string): string | null => {
  try {
    return new URL(node).host;
  } catch {
    return null;
  }
};

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(
    getStrings(config, "nodes")
      .map(formatNode)
      .filter((host): host is string => !!host)
      .join(", ") || null
  );

export const deserialize = (data: MonitorMonitorResponseDto): RabbitMQFormValues => {
  const config: RabbitMQConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "rabbitmq",
    ...deserializeMonitorBase(data),
    nodes: config.nodes ? config.nodes.map((url: string) => ({ url })) : [{ url: "https://localhost:15672" }], // Convert string[] to [{ url: "" }]
    username: config.username || "",
    password: config.password || "",
  };
};
