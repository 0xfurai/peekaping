import { defineMonitorType } from "../definition";
import { postgresSchema, postgresDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import PostgresForm from "./form";

export const postgresMonitorType = defineMonitorType({
  type: "postgres",
  labelKey: "monitors.form.type.postgres",
  schema: postgresSchema,
  defaultValues: postgresDefaultValues,
  deserialize,
  serialize,
  Form: PostgresForm,
});
