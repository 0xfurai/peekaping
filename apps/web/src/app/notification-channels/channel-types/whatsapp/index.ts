import { defineChannelType } from "../definition";
import { whatsAppSchema, whatsAppDefaultValues } from "./schema";
import WhatsAppForm from "./form";

export const whatsAppChannelType = defineChannelType({
  type: "whatsapp",
  displayName: "WhatsApp (WAHA)",
  schema: whatsAppSchema,
  defaultValues: whatsAppDefaultValues,
  Form: WhatsAppForm,
});
