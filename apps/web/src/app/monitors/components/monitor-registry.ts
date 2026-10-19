import type { MonitorMonitorResponseDto } from "@/api";
import type { MonitorForm } from "../context/monitor-form-context";
import { formatUrlTarget, type MonitorConfig, type MonitorTarget } from "./monitor-target";
import { deserialize as httpDeserialize } from "./http/schema";
import { deserialize as httpKeywordDeserialize } from "./http-keyword/schema";
import { deserialize as httpJsonQueryDeserialize } from "./http-json-query/schema";
import { deserialize as tcpDeserialize, formatTarget as tcpFormatTarget } from "./tcp";
import { deserialize as pingDeserialize, formatTarget as pingFormatTarget } from "./ping";
import { deserialize as dnsDeserialize, formatTarget as dnsFormatTarget } from "./dns";
import { deserialize as pushDeserialize } from "./push";
import { deserialize as dockerDeserialize, formatTarget as dockerFormatTarget } from "./docker";
import { deserialize as grpcKeywordDeserialize, formatTarget as grpcKeywordFormatTarget } from "./grpc-keyword";
import { deserialize as snmpDeserialize, formatTarget as snmpFormatTarget } from "./snmp";
import { deserialize as mysqlDeserialize } from "./mysql";
import { deserialize as postgresDeserialize } from "./postgres/schema";
import { deserialize as sqlServerDeserialize } from "./sqlserver/schema";
import { deserialize as mongodbDeserialize } from "./mongodb";
import { deserialize as redisDeserialize } from "./redis";
import { deserialize as mqttDeserialize, formatTarget as mqttFormatTarget } from "./mqtt";
import { deserialize as rabbitmqDeserialize, formatTarget as rabbitmqFormatTarget } from "./rabbitmq";
import { deserialize as kafkaProducerDeserialize, formatTarget as kafkaProducerFormatTarget } from "./kafka-producer/schema";
import { deserialize as smtpDeserialize, formatTarget as smtpFormatTarget } from "./smtp/schema";
import TCPForm from "./tcp";
import PingForm from "./ping";
import DNSForm from "./dns";
import HttpForm from "./http";
import HttpKeywordForm from "./http-keyword";
import HttpJsonQueryForm from "./http-json-query";
import PushForm from "./push";
import DockerForm from "./docker";
import GRPCKeywordForm from "./grpc-keyword";
import SnmpForm from "./snmp";
import MySQLForm from "./mysql";
import PostgresForm from "./postgres";
import SQLServerForm from "./sqlserver";
import MongoDBForm from "./mongodb";
import RedisForm from "./redis";
import MQTTForm from "./mqtt";
import RabbitMQForm from "./rabbitmq";
import KafkaProducerForm from "./kafka-producer";
import SMTPForm from "./smtp";

import type { ComponentType } from "react";

type DeserializeFunction = (data: MonitorMonitorResponseDto) => MonitorForm;
type MonitorComponent = ComponentType<Record<string, unknown>>;
type FormatTargetFunction = (config: MonitorConfig) => MonitorTarget | null;

interface MonitorTypeConfig {
  deserialize: DeserializeFunction;
  component: MonitorComponent;
  // Unset for types without a target that is safe to show, such as a
  // connection string with credentials or a push token
  formatTarget?: FormatTargetFunction;
}

/**
 * Unified registry of monitor types and their configurations.
 * To add a new monitor type:
 * 1. Create a deserialize function in your monitor type's schema file, and a
 *    formatTarget function if the monitor page header should show its target
 * 2. Create a React component for the monitor type
 * 3. Import them here and add them to the registry
 * 4. All functionality (cloning, forms, etc.) will automatically work!
 */
const monitorTypeRegistry: Record<string, MonitorTypeConfig> = {
  http: {
    deserialize: httpDeserialize,
    component: HttpForm,
    formatTarget: formatUrlTarget,
  },
  "http-keyword": {
    deserialize: httpKeywordDeserialize,
    component: HttpKeywordForm,
    formatTarget: formatUrlTarget,
  },
  "http-json-query": {
    deserialize: httpJsonQueryDeserialize,
    component: HttpJsonQueryForm,
    formatTarget: formatUrlTarget,
  },
  tcp: {
    deserialize: tcpDeserialize,
    component: TCPForm,
    formatTarget: tcpFormatTarget,
  },
  ping: {
    deserialize: pingDeserialize,
    component: PingForm,
    formatTarget: pingFormatTarget,
  },
  dns: {
    deserialize: dnsDeserialize,
    component: DNSForm,
    formatTarget: dnsFormatTarget,
  },
  push: {
    deserialize: pushDeserialize,
    component: PushForm,
  },
  docker: {
    deserialize: dockerDeserialize,
    component: DockerForm,
    formatTarget: dockerFormatTarget,
  },
  "grpc-keyword": {
    deserialize: grpcKeywordDeserialize,
    component: GRPCKeywordForm,
    formatTarget: grpcKeywordFormatTarget,
  },
  snmp: {
    deserialize: snmpDeserialize,
    component: SnmpForm,
    formatTarget: snmpFormatTarget,
  },
  mysql: {
    deserialize: mysqlDeserialize,
    component: MySQLForm,
  },
  postgres: {
    deserialize: postgresDeserialize,
    component: PostgresForm,
  },
  sqlserver: {
    deserialize: sqlServerDeserialize,
    component: SQLServerForm,
  },
  mongodb: {
    deserialize: mongodbDeserialize,
    component: MongoDBForm,
  },
  redis: {
    deserialize: redisDeserialize,
    component: RedisForm,
  },
  mqtt: {
    deserialize: mqttDeserialize,
    component: MQTTForm,
    formatTarget: mqttFormatTarget,
  },
  rabbitmq: {
    deserialize: rabbitmqDeserialize,
    component: RabbitMQForm,
    formatTarget: rabbitmqFormatTarget,
  },
  "kafka-producer": {
    deserialize: kafkaProducerDeserialize,
    component: KafkaProducerForm,
    formatTarget: kafkaProducerFormatTarget,
  },
  smtp: {
    deserialize: smtpDeserialize,
    component: SMTPForm,
    formatTarget: smtpFormatTarget,
  },
};

// validate registry have required fields
const validateRegistry = () => {
  const requiredFields = ["deserialize", "component"];

  Object.keys(monitorTypeRegistry).forEach(key => {
    const config = monitorTypeRegistry[key];
    requiredFields.forEach(field => {
      if (!config[field as keyof MonitorTypeConfig]) {
        throw new Error(`Missing required field ${field} in monitorTypeRegistry for ${key}`);
      }
    });
  });
};
validateRegistry()

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

export const formatMonitorTarget = (
  type: string | undefined,
  config: MonitorConfig
): MonitorTarget | null => {
  if (!type) return null;
  return monitorTypeRegistry[type]?.formatTarget?.(config) ?? null;
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
