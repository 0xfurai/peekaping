import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const twilioSchema = z.object({
  type: z.literal("twilio"),
  twilio_account_sid: z.string().min(1, {
    message: "Account SID is required",
  }),
  twilio_api_key: withSavedSecret(z.string()).optional(),
  twilio_auth_token: withSavedSecret(
    z.string().min(1, {
      message: "Auth Token is required",
    })
  ),
  twilio_from_number: z.string()
    .min(1, { message: "From Number is required" })
    .regex(/^\+[1-9]\d{1,14}$/, {
      message: "Must be a valid E.164 phone number (e.g., +1234567890)"
    }),
  twilio_to_number: z.string()
    .min(1, { message: "To Number is required" })
    .regex(/^\+[1-9]\d{1,14}$/, {
      message: "Must be a valid E.164 phone number (e.g., +1234567890)"
    }),
});

export type TwilioFormValues = z.infer<typeof twilioSchema>;

export const twilioDefaultValues: TwilioFormValues = {
  type: "twilio",
  twilio_account_sid: "",
  twilio_api_key: "",
  twilio_auth_token: "",
  twilio_from_number: "",
  twilio_to_number: "",
};
