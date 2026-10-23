import { defineChannelType } from "../definition";
import { smtpSchema, smtpDefaultValues } from "./schema";
import SmtpForm from "./form";

export const smtpChannelType = defineChannelType({
  type: "smtp",
  displayName: "Email (SMTP)",
  schema: smtpSchema,
  defaultValues: smtpDefaultValues,
  Form: SmtpForm,
});
