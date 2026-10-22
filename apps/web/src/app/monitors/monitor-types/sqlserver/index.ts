import { defineMonitorType } from "../definition";
import { sqlServerSchema, sqlServerDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import SqlServerForm from "./form";

export const sqlserverMonitorType = defineMonitorType({
  type: "sqlserver",
  labelKey: "monitors.form.type.sqlserver",
  schema: sqlServerSchema,
  defaultValues: sqlServerDefaultValues,
  deserialize,
  serialize,
  Form: SqlServerForm,
});
