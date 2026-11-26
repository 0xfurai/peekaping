import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const discordSchema = z.object({
  type: z.literal("discord"),
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),
  bot_display_name: z.string().min(1, { message: "Bot display name is required" }),
  custom_message_prefix: z.string().optional(),
  message_type: z.enum(["send_to_channel", "send_to_new_forum_post", "send_to_thread"], { message: "Message type is required" }),
  thread_name: z.string().optional(),
  thread_id: z.string().optional(),
});

export type DiscordFormValues = z.infer<typeof discordSchema>;

export const discordDefaultValues: DiscordFormValues = {
  type: "discord",
  webhook_url: "",
  bot_display_name: "Peekaping",
  custom_message_prefix: "",
  message_type: "send_to_channel",
  thread_name: "",
  thread_id: "",
};
