import { defineChannelType } from "../definition";
import { sendGridSchema, sendGridDefaultValues } from "./schema";
import SendGridForm from "./form";

export const sendGridChannelType = defineChannelType({
  type: "sendgrid",
  displayName: "SendGrid",
  schema: sendGridSchema,
  defaultValues: sendGridDefaultValues,
  Form: SendGridForm,
});
