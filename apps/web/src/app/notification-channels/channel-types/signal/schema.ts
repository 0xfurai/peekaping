import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const signalSchema = z.object({
  type: z.literal("signal"),
  signal_url: withSavedSecret(z.string().url({ message: "Valid Signal API URL is required" })),
  signal_number: z.string().min(1, { message: "Phone number is required" }),
  signal_recipients: z.string().min(1, { message: "Recipients are required" }),
  custom_message: z.string().optional(),
});

export type SignalFormValues = z.infer<typeof signalSchema>;

export const signalDefaultValues: SignalFormValues = {
  type: "signal",
  signal_url: "",
  signal_number: "",
  signal_recipients: "",
  custom_message: "{{ msg }}",
};
