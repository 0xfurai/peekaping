import { defineMonitorType } from "../definition";
import { formatUrlTarget } from "../monitor-target";
import { httpSchema, httpDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import HttpForm from "./form";

export const httpMonitorType = defineMonitorType({
  type: "http",
  labelKey: "monitors.form.type.http",
  schema: httpSchema,
  defaultValues: httpDefaultValues,
  deserialize,
  serialize,
  formatTarget: formatUrlTarget,
  Form: HttpForm,
});
