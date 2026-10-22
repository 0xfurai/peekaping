import { defineMonitorType } from "../definition";
import { kafkaProducerSchema, kafkaProducerDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import KafkaProducerForm from "./form";

export const kafkaProducerMonitorType = defineMonitorType({
  type: "kafka-producer",
  labelKey: "monitors.form.type.kafka",
  schema: kafkaProducerSchema,
  defaultValues: kafkaProducerDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: KafkaProducerForm,
});
