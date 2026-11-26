import { defineMonitorType } from "../definition";
import { mongodbSchema, mongodbDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import MongoDBForm from "./form";

export const mongodbMonitorType = defineMonitorType({
  type: "mongodb",
  labelKey: "monitors.form.type.mongodb",
  schema: mongodbSchema,
  defaultValues: mongodbDefaultValues,
  deserialize,
  serialize,
  Form: MongoDBForm,
});
