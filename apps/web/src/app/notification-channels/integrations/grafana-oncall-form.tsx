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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="grafana_oncall_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.grafana_oncall.grafana_oncall_url_label")} <span className="text-red-500">*</span>
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
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.grafana_oncall.grafana_oncall_url_description")}
              </span>
              <span className="mt-2 block">
                {t("notifications.form.grafana_oncall.learn_more_label")}:{" "}
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
