import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const teamsSchema = z.object({
  type: z.literal("teams"),
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),
  server_url: z
    .string()
    .url({ message: "Valid server URL is required" })
    .optional()
    .or(z.literal("")),
});

export type TeamsFormValues = z.infer<typeof teamsSchema>;

export const teamsDefaultValues: TeamsFormValues = {
  type: "teams",
  webhook_url: "",
  server_url: "",
};
