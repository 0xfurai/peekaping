import { defineChannelType } from "../definition";
import { teamsSchema, teamsDefaultValues } from "./schema";
import TeamsForm from "./form";

export const teamsChannelType = defineChannelType({
  type: "teams",
  displayName: "Microsoft Teams",
  schema: teamsSchema,
  defaultValues: teamsDefaultValues,
  Form: TeamsForm,
});
