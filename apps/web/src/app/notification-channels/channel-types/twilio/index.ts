import { defineChannelType } from "../definition";
import { twilioSchema, twilioDefaultValues } from "./schema";
import TwilioForm from "./form";

export const twilioChannelType = defineChannelType({
  type: "twilio",
  displayName: "Twilio",
  schema: twilioSchema,
  defaultValues: twilioDefaultValues,
  Form: TwilioForm,
});
