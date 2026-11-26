import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const grafanaOncallSchema = z.object({
  type: z.literal("grafana_oncall"),
  grafana_oncall_url: withSavedSecret(z.string().url({ message: "Valid Grafana OnCall URL is required" })),
});

export type GrafanaOncallFormValues = z.infer<typeof grafanaOncallSchema>;

export const grafanaOncallDefaultValues: GrafanaOncallFormValues = {
  type: "grafana_oncall" as const,
  grafana_oncall_url: "",
};
