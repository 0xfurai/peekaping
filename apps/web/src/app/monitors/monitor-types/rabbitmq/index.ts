import { defineMonitorType } from "../definition";
import { rabbitMQSchema, rabbitMQDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import RabbitMQForm from "./form";

export const rabbitmqMonitorType = defineMonitorType({
  type: "rabbitmq",
  labelKey: "monitors.form.type.rabbitmq",
  schema: rabbitMQSchema,
  defaultValues: rabbitMQDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: RabbitMQForm,
});
