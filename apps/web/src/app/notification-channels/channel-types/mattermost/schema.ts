import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const mattermostSchema = z.object({
  type: z.literal("mattermost"),
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),
  username: z.string().optional(),
  channel: z.string().optional(),
  icon_url: z.union([z.string().url({ message: "Valid icon URL is required" }), z.literal("")]).optional(),
  icon_emoji: z.string().optional(),
  use_template: z.boolean().optional(),
  template: z.string().optional(),
});

export type MattermostFormValues = z.infer<typeof mattermostSchema>;

export const mattermostDefaultValues: MattermostFormValues = {
  type: "mattermost" as const,
  webhook_url: "",
  username: "Peekaping",
  channel: "",
  icon_url: "",
  icon_emoji: "",
  use_template: false,
  template: "",
};
