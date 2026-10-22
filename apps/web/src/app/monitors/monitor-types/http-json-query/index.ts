import { defineMonitorType } from "../definition";
import { formatUrlTarget } from "../monitor-target";
import { httpJsonQuerySchema, httpJsonQueryDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import HttpJsonQueryForm from "./form";

export const httpJsonQueryMonitorType = defineMonitorType({
  type: "http-json-query",
  labelKey: "monitors.form.type.http_json_query",
  schema: httpJsonQuerySchema,
  defaultValues: httpJsonQueryDefaultValues,
  deserialize,
  serialize,
  formatTarget: formatUrlTarget,
  Form: HttpJsonQueryForm,
});
