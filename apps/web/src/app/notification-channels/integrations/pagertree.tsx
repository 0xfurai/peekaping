import { ExternalLink } from "lucide-react";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const pagerTreeSchema = z.object({
  type: z.literal("pagertree"),
  integrationUrl: z
    .string()
    .min(1, "Integration URL is required")
    .url("Please enter a valid URL")
    .refine(
      (url) => url.includes("api.pagertree.com/integration/"),
      "URL must be a valid PagerTree integration endpoint"
    ),
  urgency: z.enum(["silent", "low", "medium", "high", "critical"]),
  autoResolve: z.boolean(),
  authToken: z.string().optional(),
});

export type PagerTreeConfig = z.infer<typeof pagerTreeSchema>;

export const PagerTreeFormDefaultValues: PagerTreeConfig = {
  type: "pagertree",
  integrationUrl: "",
  urgency: "medium",
  autoResolve: true,
  authToken: "",
};

export const PagerTreeDisplayName = "PagerTree";

const PagerTreeForm = () => {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();
  
  return (
    <>
      <FormField
        control={form.control}
        name="integrationUrl"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.pagertree.integration_url_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://api.pagertree.com/integration/..."
                required
                {...field}
                autoComplete="integration-url"
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pagertree.integration_url_description")}{" "}
              <a
                href="https://pagertree.com/docs/integration-guides/introduction#copy-the-endpoint-url"
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-primary hover:underline"
              >
                {t("notifications.form.pagertree.how_to_get_integration_url_label")}
                <ExternalLink className="h-3 w-3" />
              </a>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="urgency"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pagertree.urgency_level_label")}</FormLabel>
            <Select onValueChange={field.onChange} value={field.value || "medium"}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pagertree.urgency_level_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="silent">{t("notifications.form.pagertree.urgency_level_silent")}</SelectItem>
                <SelectItem value="low">{t("notifications.form.pagertree.urgency_level_low")}</SelectItem>
                <SelectItem value="medium">{t("notifications.form.pagertree.urgency_level_medium")}</SelectItem>
                <SelectItem value="high">{t("notifications.form.pagertree.urgency_level_high")}</SelectItem>
                <SelectItem value="critical">{t("notifications.form.pagertree.urgency_level_critical")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pagertree.urgency_level_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="autoResolve"
        render={({ field }) => (
          <FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
            <div className="space-y-0.5">
              <FormLabel>{t("notifications.form.pagertree.auto_resolve_alerts_label")}</FormLabel>
              <FormDescription>
                {t("notifications.form.pagertree.auto_resolve_alerts_description")}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value || false}
                onCheckedChange={field.onChange}
              />
            </FormControl>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authToken"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pagertree.authentication_token_label")}</FormLabel>
            <FormControl>
              <PasswordInput
                placeholder={t("notifications.form.pagertree.authentication_token_placeholder")}
                {...field}
                autoComplete="new-password"
                value={field.value || ""}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pagertree.authentication_token_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

// Export for registry
export default PagerTreeForm;
export const schema = pagerTreeSchema;
export const displayName = PagerTreeDisplayName;
export const defaultValues = PagerTreeFormDefaultValues;
