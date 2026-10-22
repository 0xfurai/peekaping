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
import { SecretInput } from "../components/saved-secrets";
import { withSavedSecret } from "../secrets";
import { useFormContext } from "react-hook-form";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { InfoIcon } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("teams"),
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),
  server_url: z
    .string()
    .url({ message: "Valid server URL is required" })
    .optional()
    .or(z.literal("")),
});

export type TeamsFormValues = z.infer<typeof schema>;

export const defaultValues: TeamsFormValues = {
  type: "teams",
  webhook_url: "",
  server_url: "",
};

export const displayName = "Microsoft Teams";

export default function TeamsForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.teams.webhook_url_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <SecretInput
                placeholder="https://…powerplatform.com/powerautomate/automations/direct/workflows/…"
                autoComplete="off"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
            </FormDescription>
            <FormMessage />
            <Alert>
              <InfoIcon className="mr-2 h-4 w-4" />
              <AlertTitle className="font-bold">
                {t("notifications.form.teams.setup_webhook_title")}
              </AlertTitle>
              <AlertDescription>
                <ul className="list-inside list-disc text-sm mt-2">
                  <li>{t("notifications.form.teams.setup_webhook_description_1")}</li>
                  <li>{t("notifications.form.teams.setup_webhook_description_2")}</li>
                </ul>
                <span className="mt-2 block">
                  {t("notifications.form.teams.more_info_about_webhooks")}:{" "}
                  <a
                    href="https://support.microsoft.com/en-us/office/create-incoming-webhooks-with-workflows-for-microsoft-teams-8ae491c7-0394-4861-ba59-055e33f75498"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline text-blue-600"
                  >
                    support.microsoft.com
                  </a>
                </span>
              </AlertDescription>
            </Alert>
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.teams.server_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="https://peekaping.example.com"
                type="url"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.teams.server_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
