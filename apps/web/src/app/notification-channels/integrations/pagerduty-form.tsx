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
import { useFormContext } from "react-hook-form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export const schema = z.object({
  type: z.literal("pagerduty"),
  pagerduty_integration_key: z.string().min(1, { message: "Integration key is required" }),
  pagerduty_integration_url: z.string().url({ message: "Valid integration URL is required" }),
  pagerduty_priority: z.string().optional(),
  pagerduty_auto_resolve: z.string().optional(),
});

export type PagerDutyFormValues = z.infer<typeof schema>;

export const defaultValues: PagerDutyFormValues = {
  type: "pagerduty",
  pagerduty_integration_key: "",
  pagerduty_integration_url: "https://events.pagerduty.com/v2/enqueue",
  pagerduty_priority: "warning",
  pagerduty_auto_resolve: "0",
};

export const displayName = "PagerDuty";

export default function PagerDutyForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="pagerduty_integration_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Integration Key <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="Enter your PagerDuty integration key"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              <span className="mt-2 block">
                Learn how to get your integration key:{" "}
                <a
                  href="https://support.pagerduty.com/docs/services-and-integrations"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  PagerDuty Documentation
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pagerduty_integration_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Integration URL <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://events.pagerduty.com/v2/enqueue"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              The PagerDuty Events API v2 endpoint URL. Defaults to the standard endpoint.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pagerduty_priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Priority</FormLabel>
            <Select onValueChange={field.onChange} defaultValue={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select priority level" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="info">Info</SelectItem>
                <SelectItem value="warning">Warning</SelectItem>
                <SelectItem value="error">Error</SelectItem>
                <SelectItem value="critical">Critical</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              The severity level for PagerDuty incidents. Defaults to "warning".
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pagerduty_auto_resolve"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Auto Resolve or Acknowledge</FormLabel>
            <Select onValueChange={field.onChange} defaultValue={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select auto-resolve behavior" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="0">Do nothing</SelectItem>
                <SelectItem value="acknowledge">Auto acknowledge</SelectItem>
                <SelectItem value="resolve">Auto resolve</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              Choose what action to take when a monitor comes back up:
              <br />
              • <strong>Do nothing:</strong> No action taken on UP status
              <br />
              • <strong>Auto acknowledge:</strong> Automatically acknowledge the incident
              <br />
              • <strong>Auto resolve:</strong> Automatically resolve the incident
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
