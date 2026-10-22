import { defineMonitorType } from "../definition";
import { smtpSchema, smtpDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import SmtpForm from "./form";

export const smtpMonitorType = defineMonitorType({
  type: "smtp",
  labelKey: "monitors.form.type.smtp",
  schema: smtpSchema,
  defaultValues: smtpDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: SmtpForm,
});
