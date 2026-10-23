import { defineChannelType } from "../definition";
import { slackSchema, slackDefaultValues, refine } from "./schema";
import SlackForm from "./form";

export const slackChannelType = defineChannelType({
  type: "slack",
  displayName: "Slack",
  schema: slackSchema,
  defaultValues: slackDefaultValues,
  refine,
  Form: SlackForm,
});
