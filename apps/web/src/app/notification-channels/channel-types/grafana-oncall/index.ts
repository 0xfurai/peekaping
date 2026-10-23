import { defineChannelType } from "../definition";
import { grafanaOncallSchema, grafanaOncallDefaultValues } from "./schema";
import GrafanaOncallForm from "./form";

export const grafanaOncallChannelType = defineChannelType({
  type: "grafana_oncall",
  displayName: "Grafana OnCall",
  schema: grafanaOncallSchema,
  defaultValues: grafanaOncallDefaultValues,
  Form: GrafanaOncallForm,
});
