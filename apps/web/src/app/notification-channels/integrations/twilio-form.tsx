import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

export const schema = z.object({
  type: z.literal("twilio"),
  twilio_account_sid: z.string().min(1, {
    message: "Account SID is required",
  }),
  twilio_api_key: z.string().optional(),
  twilio_auth_token: z.string().min(1, {
    message: "Auth Token is required",
  }),
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

export type TwilioFormValues = z.infer<typeof schema>;

export const defaultValues: TwilioFormValues = {
  type: "twilio",
  twilio_account_sid: "",
  twilio_api_key: "",
  twilio_auth_token: "",
  twilio_from_number: "",
  twilio_to_number: "",
};

export const displayName = "Twilio";

export default function TwilioForm() {
  const form = useFormContext();

  return (
    <div className="space-y-6">
      <FormField
        control={form.control}
        name="twilio_account_sid"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Account SID</FormLabel>
            <FormControl>
              <Input
                placeholder="AC1234567890abcdef1234567890abcdef"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Your Twilio Account SID from the Twilio Console
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="twilio_api_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>API Key (optional)</FormLabel>
            <FormControl>
              <Input
                placeholder="SK1234567890abcdef1234567890abcdef"
                {...field}
              />
            </FormControl>
            <FormDescription>
              The API key is optional but recommended. You can provide either Account SID and AuthToken
              from the Twilio Console page or Account SID and the pair of API Key and API Key Secret
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="twilio_auth_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Auth Token / API Key Secret</FormLabel>
            <FormControl>
              <Input
                type="password"
                placeholder="Your Auth Token or API Key Secret"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Your Twilio Auth Token or API Key Secret
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="twilio_from_number"
        render={({ field }) => (
          <FormItem>
            <FormLabel>From Number</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Your Twilio phone number in E.164 format (e.g., +1234567890)
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="twilio_to_number"
        render={({ field }) => (
          <FormItem>
            <FormLabel>To Number</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890"
                {...field}
              />
            </FormControl>
            <FormDescription>
              The recipient phone number in E.164 format (e.g., +1234567890)
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="text-sm text-muted-foreground">
        <p>
          More info on:{" "}
          <a
            href="https://www.twilio.com/docs/sms"
            target="_blank"
            rel="noopener noreferrer"
            className="underline"
          >
            https://www.twilio.com/docs/sms
          </a>
        </p>
      </div>
    </div>
  );
}
