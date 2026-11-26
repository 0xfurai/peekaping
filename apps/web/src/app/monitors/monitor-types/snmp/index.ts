import { defineMonitorType } from "../definition";
import { snmpSchema, snmpDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import SnmpForm from "./form";

export const snmpMonitorType = defineMonitorType({
  type: "snmp",
  labelKey: "monitors.form.type.snmp",
  schema: snmpSchema,
  defaultValues: snmpDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: SnmpForm,
});
