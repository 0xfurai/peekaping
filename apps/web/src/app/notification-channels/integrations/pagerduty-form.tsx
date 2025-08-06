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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="pagerduty_integration_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.pagerduty.integration_key_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.pagerduty.integration_key_placeholder")}
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.pagerduty.integration_key_description")}:{" "}
                <a
                  href="https://support.pagerduty.com/docs/services-and-integrations"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  {t("notifications.form.pagerduty.learn_more_label")}
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
              {t("notifications.form.pagerduty.integration_url_label")} <span className="text-red-500">*</span>
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
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              {t("notifications.form.pagerduty.integration_url_description")}
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
            <FormLabel>{t("notifications.form.pagerduty.priority_label")}</FormLabel>
            <Select onValueChange={field.onChange} defaultValue={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pagerduty.priority_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="info">{t("notifications.form.pagerduty.priority_info")}</SelectItem>
                <SelectItem value="warning">{t("notifications.form.pagerduty.priority_warning")}</SelectItem>
                <SelectItem value="error">{t("notifications.form.pagerduty.priority_error")}</SelectItem>
                <SelectItem value="critical">{t("notifications.form.pagerduty.priority_critical")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pagerduty.priority_description")}
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
            <FormLabel>{t("notifications.form.pagerduty.auto_resolve_label")}</FormLabel>
            <Select onValueChange={field.onChange} defaultValue={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pagerduty.auto_resolve_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="0">{t("notifications.form.pagerduty.auto_resolve_do_nothing")}</SelectItem>
                <SelectItem value="acknowledge">{t("notifications.form.pagerduty.auto_resolve_acknowledge")}</SelectItem>
                <SelectItem value="resolve">{t("notifications.form.pagerduty.auto_resolve_resolve")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pagerduty.auto_resolve_description")}
              <br />
              • <strong>{t("notifications.form.pagerduty.auto_resolve_do_nothing")}:</strong> {t("notifications.form.pagerduty.auto_resolve_do_nothing_description")}
              <br />
              • <strong>{t("notifications.form.pagerduty.auto_resolve_acknowledge")}:</strong> {t("notifications.form.pagerduty.auto_resolve_acknowledge_description")}
              <br />
              • <strong>{t("notifications.form.pagerduty.auto_resolve_resolve")}:</strong> {t("notifications.form.pagerduty.auto_resolve_resolve_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
