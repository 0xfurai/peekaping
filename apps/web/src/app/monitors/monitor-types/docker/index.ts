import { defineMonitorType } from "../definition";
import { dockerSchema, dockerDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import DockerForm from "./form";

export const dockerMonitorType = defineMonitorType({
  type: "docker",
  labelKey: "monitors.form.type.docker",
  schema: dockerSchema,
  defaultValues: dockerDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: DockerForm,
});
