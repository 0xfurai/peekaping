import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const slackSchema = z.object({
  type: z.literal("slack"),
  slack_webhook_url: withSavedSecret(
    z.union([z.string().url({ message: "Valid webhook URL is required" }), z.literal("")])
  ).optional(),
  slack_bot_token: withSavedSecret(z.string().trim()).optional(),
  slack_username: z.string().optional(),
  slack_icon_emoji: z.string().optional(),
  slack_channel: z.string().trim().optional(),
  slack_rich_message: z.boolean().optional(),
  slack_channel_notify: z.boolean().optional(),
});

export type SlackFormValues = z.infer<typeof slackSchema>;

// Cross-field rules, applied by the notification form's superRefine: a refined
// slackSchema can't be a member of its discriminated union.
export function refine(data: SlackFormValues, ctx: z.RefinementCtx) {
  if (!data.slack_webhook_url && !data.slack_bot_token) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: "Either Webhook URL or Bot Token must be provided",
      path: ["slack_webhook_url"],
    });
  }
  if (data.slack_bot_token && !data.slack_channel) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: "Channel is required when using Bot Token",
      path: ["slack_channel"],
    });
  }
}

export const slackDefaultValues: SlackFormValues = {
  type: "slack",
  slack_webhook_url: "",
  slack_bot_token: "",
  slack_username: "",
  slack_icon_emoji: "",
  slack_channel: "",
  slack_rich_message: false,
  slack_channel_notify: false,
};
