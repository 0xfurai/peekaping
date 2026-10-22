import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const lineSchema = z.object({
  type: z.literal("line"),
  channel_access_token: withSavedSecret(z.string().min(1, { message: "Channel access token is required" })),
  user_id: z.string().min(1, { message: "User ID is required" }),
  template: z.string().optional(),
});

export type LineFormValues = z.infer<typeof lineSchema>;

export const lineDefaultValues: LineFormValues = {
  type: "line",
  channel_access_token: "",
  user_id: "",
  template: `Peekaping Alert - {{ monitor.name }}

Status: {{ status }}
{{ msg }}`,
};
