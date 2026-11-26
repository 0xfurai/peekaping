import { defineChannelType } from "../definition";
import { discordSchema, discordDefaultValues } from "./schema";
import DiscordForm from "./form";

export const discordChannelType = defineChannelType({
  type: "discord",
  displayName: "Discord",
  schema: discordSchema,
  defaultValues: discordDefaultValues,
  Form: DiscordForm,
});
