import { defineMonitorType } from "../definition";
import { pushSchema, pushDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import PushForm from "./form";

export const pushMonitorType = defineMonitorType({
  type: "push",
  labelKey: "monitors.form.type.push",
  schema: pushSchema,
  defaultValues: pushDefaultValues,
  deserialize,
  serialize,
  Form: PushForm,
});
