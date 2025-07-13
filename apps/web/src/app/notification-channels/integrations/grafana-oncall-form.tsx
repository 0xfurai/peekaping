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

export const schema = z.object({
  type: z.literal("grafana_oncall"),
  grafana_oncall_url: z.string().url({ message: "Valid Grafana OnCall URL is required" }),
});

export const defaultValues = {
  type: "grafana_oncall" as const,
  grafana_oncall_url: "",
};

export const displayName = "Grafana OnCall";

export default function GrafanaOncallForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="grafana_oncall_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Grafana OnCall URL <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://your-grafana-oncall-instance.com/integrations/v1/webhook/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              <span className="mt-2 block">
                The webhook URL from your Grafana OnCall integration. You can find this in your Grafana OnCall instance under Alerts & IRM &gt; IRM &gt; Integrations &gt; Webhook.
              </span>
              <span className="mt-2 block">
                Learn more about Grafana OnCall webhooks:{" "}
                <a
                  href="https://grafana.com/docs/oncall/latest/integrations/webhook/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://grafana.com/docs/oncall/latest/integrations/webhook/
                </a>

              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
