import { defineChannelType } from "../definition";
import { webhookSchema, webhookDefaultValues } from "./schema";
import WebhookForm from "./form";

export const webhookChannelType = defineChannelType({
  type: "webhook",
  displayName: "Webhook",
  schema: webhookSchema,
  defaultValues: webhookDefaultValues,
  Form: WebhookForm,
});
