import { defineMonitorType } from "../definition";
import { pingSchema, pingDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import PingForm from "./form";

export const pingMonitorType = defineMonitorType({
  type: "ping",
  labelKey: "monitors.form.type.ping",
  schema: pingSchema,
  defaultValues: pingDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: PingForm,
});
