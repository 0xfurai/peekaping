import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const telegramSchema = z.object({
  type: z.literal("telegram"),
  bot_token: withSavedSecret(z.string().min(1, { message: "Bot token is required" })),
  chat_id: z.string().min(1, { message: "Chat ID is required" }),
  message_thread_id: z.string().optional(),
  server_url: z.string().min(1, { message: "Server URL is required" }),
  use_template: z.boolean().optional(),
  template_parse_mode: z.enum(["plain", "HTML", "MarkdownV2"]).optional(),
  template: z.string().optional(),
  send_silently: z.boolean().optional(),
  protect_content: z.boolean().optional(),
});

export type TelegramFormValues = z.infer<typeof telegramSchema>;

export const telegramDefaultValues: TelegramFormValues = {
  type: "telegram",
  bot_token: "",
  chat_id: "",
  message_thread_id: "",
  server_url: "https://api.telegram.org",
  use_template: false,
  template_parse_mode: "plain",
  template: `Peekaping Alert - {{ monitor.name }}\n\n{{ msg }}`,
  send_silently: false,
  protect_content: false,
};
