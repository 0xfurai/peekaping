import { defineMonitorType } from "../definition";
import { mysqlSchema, mysqlDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import MySQLForm from "./form";

export const mysqlMonitorType = defineMonitorType({
  type: "mysql",
  labelKey: "monitors.form.type.mysql",
  schema: mysqlSchema,
  defaultValues: mysqlDefaultValues,
  deserialize,
  serialize,
  Form: MySQLForm,
});
