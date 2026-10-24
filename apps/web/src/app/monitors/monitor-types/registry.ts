import { z } from "zod";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MonitorConfig, MonitorTarget } from "./monitor-target";
import { httpMonitorType } from "./http";
import { httpKeywordMonitorType } from "./http-keyword";
import { httpJsonQueryMonitorType } from "./http-json-query";
import { tcpMonitorType } from "./tcp";
import { pingMonitorType } from "./ping";
import { dnsMonitorType } from "./dns";
import { pushMonitorType } from "./push";
import { dockerMonitorType } from "./docker";
import { grpcKeywordMonitorType } from "./grpc-keyword";
import { snmpMonitorType } from "./snmp";
import { mysqlMonitorType } from "./mysql";
import { postgresMonitorType } from "./postgres";
import { sqlserverMonitorType } from "./sqlserver";
import { mongodbMonitorType } from "./mongodb";
import { redisMonitorType } from "./redis";
import { mqttMonitorType } from "./mqtt";
import { rabbitmqMonitorType } from "./rabbitmq";
import { kafkaProducerMonitorType } from "./kafka-producer";
import { smtpMonitorType } from "./smtp";
import { locationThresholdError } from "./shared/location-rule-support";

/**
 * Every monitor type, in the order the Type select lists them. This is the one list: the form
 * schema, the form values type, the select options, cloning and the page header target all come
 * from it. To add a type, create its folder next to the others (schema.ts, codec.ts, form.tsx and
 * an index.ts with its definition; see docs/adr/0004-web-form-conventions.md) and add it here.
 */
export const monitorTypes = [
  httpMonitorType,
  httpKeywordMonitorType,
  httpJsonQueryMonitorType,
  tcpMonitorType,
  pingMonitorType,
  dnsMonitorType,
  pushMonitorType,
  dockerMonitorType,
  grpcKeywordMonitorType,
  snmpMonitorType,
  mysqlMonitorType,
  postgresMonitorType,
  sqlserverMonitorType,
  mongodbMonitorType,
  redisMonitorType,
  mqttMonitorType,
  rabbitmqMonitorType,
  kafkaProducerMonitorType,
  smtpMonitorType,
] as const;

type MonitorTypeDefinitions = (typeof monitorTypes)[number];

export type MonitorType = MonitorTypeDefinitions["type"];

// The values of the monitor form: one type's values at a time, told apart by `type`
export type MonitorFormValues = z.infer<MonitorTypeDefinitions["schema"]>;

type SchemasOf<Definitions extends readonly { schema: unknown }[]> = {
  -readonly [K in keyof Definitions]: Definitions[K] extends { schema: infer S } ? S : never;
};
type MonitorTypeSchemas = SchemasOf<typeof monitorTypes>;

// The form's schema: the `type` field picks the type's schema
export const monitorFormSchema = z.discriminatedUnion(
  "type",
  monitorTypes.map((definition) => definition.schema) as MonitorTypeSchemas
);

// The form schema with the checks that need more than one field: a threshold can't be higher than
// the number of probes the monitor runs on
export const checkedMonitorFormSchema = monitorFormSchema.superRefine((values, ctx) => {
  const message = locationThresholdError(values);
  if (message) ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["location_threshold"], message });
});

const byType = new Map<string, MonitorTypeDefinitions>(
  monitorTypes.map((definition) => [definition.type, definition])
);

export const getMonitorType = (type: string | undefined): MonitorTypeDefinitions | undefined =>
  type ? byType.get(type) : undefined;

export const deserializeMonitor = (data: MonitorMonitorResponseDto): MonitorFormValues => {
  if (!data.type) {
    throw new Error("Monitor type is required");
  }
  const definition = getMonitorType(data.type);
  if (!definition) {
    throw new Error(`No configuration found for monitor type: ${data.type}`);
  }
  return definition.deserialize(data);
};

export const serializeMonitor = (values: MonitorFormValues): MonitorCreateUpdateDto => {
  const definition = getMonitorType(values.type);
  if (!definition) {
    throw new Error(`No configuration found for monitor type: ${values.type}`);
  }
  // The union's `type` chose the definition, so its serializer takes these values
  return (definition.serialize as (values: MonitorFormValues) => MonitorCreateUpdateDto)(values);
};

export const formatMonitorTarget = (
  type: string | undefined,
  config: MonitorConfig
): MonitorTarget | null => getMonitorType(type)?.formatTarget?.(config) ?? null;

export const cloneMonitor = (
  data: MonitorMonitorResponseDto | undefined
): MonitorFormValues | undefined => {
  if (!data) {
    return;
  }
  return deserializeMonitor({ ...data, name: `${data.name || "Monitor"} Copy` });
};
