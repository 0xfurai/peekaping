import { defineMonitorType } from "../definition";
import { mqttSchema, mqttDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import MqttForm from "./form";

export const mqttMonitorType = defineMonitorType({
  type: "mqtt",
  labelKey: "monitors.form.type.mqtt",
  schema: mqttSchema,
  defaultValues: mqttDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: MqttForm,
});
