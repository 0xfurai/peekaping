import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const opsgenieSchema = z.object({
  type: z.literal("opsgenie"),
  region: z.enum(["us", "eu"], { message: "Region is required" }),
  api_key: withSavedSecret(z.string().min(1, { message: "API key is required" })),
  priority: z.number().min(1).max(5).optional(),
});

export type OpsgenieFormValues = z.infer<typeof opsgenieSchema>;

export const opsgenieDefaultValues: OpsgenieFormValues = {
  type: "opsgenie",
  region: "us",
  api_key: "",
  priority: 3,
};
