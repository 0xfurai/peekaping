import { defineMonitorType } from "../definition";
import { formatUrlTarget } from "../monitor-target";
import { httpKeywordSchema, httpKeywordDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import HttpKeywordForm from "./form";

export const httpKeywordMonitorType = defineMonitorType({
  type: "http-keyword",
  labelKey: "monitors.form.type.http_keyword",
  schema: httpKeywordSchema,
  defaultValues: httpKeywordDefaultValues,
  deserialize,
  serialize,
  formatTarget: formatUrlTarget,
  Form: HttpKeywordForm,
});
