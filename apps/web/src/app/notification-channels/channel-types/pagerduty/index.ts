import { defineChannelType } from "../definition";
import { pagerDutySchema, pagerDutyDefaultValues } from "./schema";
import PagerDutyForm from "./form";

export const pagerDutyChannelType = defineChannelType({
  type: "pagerduty",
  displayName: "PagerDuty",
  schema: pagerDutySchema,
  defaultValues: pagerDutyDefaultValues,
  Form: PagerDutyForm,
});
