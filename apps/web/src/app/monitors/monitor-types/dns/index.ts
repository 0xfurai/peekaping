import { defineMonitorType } from "../definition";
import { dnsSchema, dnsDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import DnsForm from "./form";

export const dnsMonitorType = defineMonitorType({
  type: "dns",
  labelKey: "monitors.form.type.dns",
  schema: dnsSchema,
  defaultValues: dnsDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: DnsForm,
});
