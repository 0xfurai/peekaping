import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const pagerDutySchema = z.object({
  type: z.literal("pagerduty"),
  pagerduty_integration_key: withSavedSecret(z.string().min(1, { message: "Integration key is required" })),
  pagerduty_integration_url: withSavedSecret(z.string().url({ message: "Valid integration URL is required" })),
  pagerduty_priority: z.string().optional(),
  pagerduty_auto_resolve: z.string().optional(),
});

export type PagerDutyFormValues = z.infer<typeof pagerDutySchema>;

export const pagerDutyDefaultValues: PagerDutyFormValues = {
  type: "pagerduty",
  pagerduty_integration_key: "",
  pagerduty_integration_url: "https://events.pagerduty.com/v2/enqueue",
  pagerduty_priority: "warning",
  pagerduty_auto_resolve: "0",
};
