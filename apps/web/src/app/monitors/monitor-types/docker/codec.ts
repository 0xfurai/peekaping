import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { DockerFormValues } from "./schema";
import { formatOnServer, getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface DockerConfig {
  container_id: string;
  connection_type: string;
  docker_daemon: string;
  // TLS fields
  tls_enabled?: boolean;
  tls_cert?: string;
  tls_key?: string;
  tls_ca?: string;
  tls_verify?: boolean;
}

// Names the daemon too, so the same container on two daemons can be told apart
export const formatTarget = (config: MonitorConfig): MonitorTarget | null => {
  const container = getString(config, "container_id");
  if (!container) return null;
  return textTarget(formatOnServer(container, getString(config, "docker_daemon")));
};

export const deserialize = (data: MonitorMonitorResponseDto): DockerFormValues => {
  let config: DockerConfig = {
    container_id: "",
    connection_type: "socket",
    docker_daemon: "/var/run/docker.sock",
    tls_enabled: false,
    tls_cert: "",
    tls_key: "",
    tls_ca: "",
    tls_verify: true,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        container_id: parsedConfig.container_id || "",
        connection_type: parsedConfig.connection_type || "socket",
        docker_daemon: parsedConfig.docker_daemon || "/var/run/docker.sock",
        tls_enabled: parsedConfig.tls_enabled || false,
        tls_cert: parsedConfig.tls_cert || "",
        tls_key: parsedConfig.tls_key || "",
        tls_ca: parsedConfig.tls_ca || "",
        tls_verify: parsedConfig.tls_verify !== undefined ? parsedConfig.tls_verify : true,
      };
    } catch (error) {
      console.error("Failed to parse Docker monitor config:", error);
    }
  }

  return {
    type: "docker",
    ...deserializeMonitorBase(data),
    container_id: config.container_id,
    connection_type: config.connection_type as DockerFormValues["connection_type"],
    docker_daemon: config.docker_daemon,
    tls_enabled: config.tls_enabled || false,
    tls_cert: config.tls_cert || "",
    tls_key: config.tls_key || "",
    tls_ca: config.tls_ca || "",
    tls_verify: config.tls_verify !== undefined ? config.tls_verify : true,
    proxy_id: data.proxy_id || "",
  };
};

export const serialize = (values: DockerFormValues): MonitorCreateUpdateDto => {
  const config: DockerConfig = {
    container_id: values.container_id,
    connection_type: values.connection_type,
    docker_daemon: values.docker_daemon,
    tls_enabled: values.tls_enabled,
    ...(values.tls_enabled && {
      tls_cert: values.tls_cert,
      tls_key: values.tls_key,
      tls_ca: values.tls_ca,
      tls_verify: values.tls_verify,
    }),
  };

  return {
    type: "docker",
    ...serializeMonitorBase(values),
    proxy_id: values.proxy_id,
    config: JSON.stringify(config),
  };
};
