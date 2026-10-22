import { defineChannelType } from "../definition";
import { telegramSchema, telegramDefaultValues } from "./schema";
import TelegramForm from "./form";

export const telegramChannelType = defineChannelType({
  type: "telegram",
  displayName: "Telegram",
  schema: telegramSchema,
  defaultValues: telegramDefaultValues,
  Form: TelegramForm,
});
