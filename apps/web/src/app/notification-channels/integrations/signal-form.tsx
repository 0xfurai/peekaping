import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { Textarea } from "@/components/ui/textarea";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("signal"),
  signal_url: z.string().url({ message: "Valid Signal API URL is required" }),
  signal_number: z.string().min(1, { message: "Phone number is required" }),
  signal_recipients: z.string().min(1, { message: "Recipients are required" }),
  custom_message: z.string().optional(),
});

export type SignalFormValues = z.infer<typeof schema>;

export const defaultValues: SignalFormValues = {
  type: "signal",
  signal_url: "",
  signal_number: "",
  signal_recipients: "",
  custom_message: "{{ msg }}",
};

export const displayName = "Signal";

export default function SignalForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="signal_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Post URL</FormLabel>
            <FormControl>
              <Input
                placeholder="http://localhost:8080/v2/send"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The URL of your Signal CLI REST API server.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="signal_number"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Number</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The phone number of your Signal account (sender number).
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="signal_recipients"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Recipients</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890,+0987654321"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              Comma-separated list of phone numbers to send notifications to.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Message</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ name }} is {{ status }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Custom message template. Available variables: {"{{ msg }}"}, {"{{ name }}"}, {"{{ status }}"}, {"{{ monitor.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800">
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <strong>Note:</strong> You need to have a Signal client with REST API.
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          You can check this URL to view how to set one up:
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <a
            href="https://github.com/bbernhard/signal-cli-rest-api"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-amber-900 dark:hover:text-amber-100"
          >
            https://github.com/bbernhard/signal-cli-rest-api
          </a>
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <strong>IMPORTANT:</strong> You cannot mix groups and numbers in recipients!
        </p>
      </div>
    </>
  );
}
