import { defineChannelType } from "../definition";
import { pushoverSchema, pushoverDefaultValues } from "./schema";
import PushoverForm from "./form";

export const pushoverChannelType = defineChannelType({
  type: "pushover",
  displayName: "Pushover",
  schema: pushoverSchema,
  defaultValues: pushoverDefaultValues,
  Form: PushoverForm,
});
