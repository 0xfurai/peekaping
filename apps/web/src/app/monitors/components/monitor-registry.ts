import type { MonitorMonitorResponseDto } from "@/api";
import type { MonitorForm } from "../context/monitor-form-context";
import { deserialize as httpDeserialize } from "./http/schema";
import { deserialize as tcpDeserialize } from "./tcp";
import { deserialize as pingDeserialize } from "./ping";
import { deserialize as dnsDeserialize } from "./dns";
import { deserialize as pushDeserialize } from "./push";
import { deserialize as dockerDeserialize } from "./docker";
import { deserialize as grpcKeywordDeserialize } from "./grpc-keyword";
import { deserialize as snmpDeserialize } from "./snmp";
import { deserialize as mysqlDeserialize } from "./mysql";
import TCPForm from "./tcp";
import PingForm from "./ping";
import DNSForm from "./dns";
import HttpForm from "./http";
import PushForm from "./push";
import DockerForm from "./docker";
import GRPCKeywordForm from "./grpc-keyword";
import SnmpForm from "./snmp";
import MySQLForm from "./mysql";

import type { ComponentType } from "react";

type DeserializeFunction = (data: MonitorMonitorResponseDto) => MonitorForm;
type MonitorComponent = ComponentType<Record<string, unknown>>;

interface MonitorTypeConfig {
  deserialize: DeserializeFunction;
  component: MonitorComponent;
}

/**
 * Unified registry of monitor types and their configurations.
 * To add a new monitor type:
 * 1. Create a deserialize function in your monitor type's schema file
 * 2. Create a React component for the monitor type
 * 3. Import both here and add them to the registry
 * 4. All functionality (cloning, forms, etc.) will automatically work!
 */
const monitorTypeRegistry: Record<string, MonitorTypeConfig> = {
  http: {
    deserialize: httpDeserialize,
    component: HttpForm,
  },
  tcp: {
    deserialize: tcpDeserialize,
    component: TCPForm,
  },
  ping: {
    deserialize: pingDeserialize,
    component: PingForm,
  },
  dns: {
    deserialize: dnsDeserialize,
    component: DNSForm,
  },
  push: {
    deserialize: pushDeserialize,
    component: PushForm,
  },
  docker: {
    deserialize: dockerDeserialize,
    component: DockerForm,
  },
  "grpc-keyword": {
    deserialize: grpcKeywordDeserialize,
    component: GRPCKeywordForm,
  },
  snmp: {
    deserialize: snmpDeserialize,
    component: SnmpForm,
  },
  mysql: {
    deserialize: mysqlDeserialize,
    component: MySQLForm,
  },
};

export const deserializeMonitor = (data: MonitorMonitorResponseDto): MonitorForm => {
  if (!data.type) {
    throw new Error("Monitor type is required");
  }

  const config = monitorTypeRegistry[data.type];

  if (!config) {
    throw new Error(`No configuration found for monitor type: ${data.type}`);
  }

  return config.deserialize(data);
};

export const getMonitorComponent = (type: string): MonitorComponent | null => {
  const config = monitorTypeRegistry[type];
  return config?.component || null;
};

export const getSupportedMonitorTypes = (): string[] => {
  return Object.keys(monitorTypeRegistry);
};

export const cloneMonitor = (data: MonitorMonitorResponseDto | undefined): MonitorForm | undefined => {
  if (!data) {
    return;
  }

  const clonedData = {
    ...data,
    name: `${data.name || "Monitor"} Copy`,
  };

  return deserializeMonitor(clonedData);
};
