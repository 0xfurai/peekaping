import { defineChannelType } from "../definition";
import { mattermostSchema, mattermostDefaultValues } from "./schema";
import MattermostForm from "./form";

export const mattermostChannelType = defineChannelType({
  type: "mattermost",
  displayName: "Mattermost",
  schema: mattermostSchema,
  defaultValues: mattermostDefaultValues,
  Form: MattermostForm,
});
