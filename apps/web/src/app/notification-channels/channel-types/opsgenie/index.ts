import { defineChannelType } from "../definition";
import { opsgenieSchema, opsgenieDefaultValues } from "./schema";
import OpsgenieForm from "./form";

export const opsgenieChannelType = defineChannelType({
  type: "opsgenie",
  displayName: "Opsgenie",
  schema: opsgenieSchema,
  defaultValues: opsgenieDefaultValues,
  Form: OpsgenieForm,
});
