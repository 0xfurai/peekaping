import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const pagerTreeSchema = z.object({
  type: z.literal("pagertree"),
  integrationUrl: withSavedSecret(
    z
      .string()
      .min(1, "Integration URL is required")
      .url("Please enter a valid URL")
      .refine(
        (url) => url.includes("api.pagertree.com/integration/"),
        "URL must be a valid PagerTree integration endpoint"
      )
  ),
  urgency: z.enum(["silent", "low", "medium", "high", "critical"]),
  autoResolve: z.boolean(),
  authToken: withSavedSecret(z.string()).optional(),
});

export type PagerTreeFormValues = z.infer<typeof pagerTreeSchema>;

export const pagerTreeDefaultValues: PagerTreeFormValues = {
  type: "pagertree",
  integrationUrl: "",
  urgency: "medium",
  autoResolve: true,
  authToken: "",
};
