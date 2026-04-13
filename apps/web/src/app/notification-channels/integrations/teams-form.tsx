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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { InfoIcon } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("teams"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
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
              {t("notifications.form.teams.webhook_url_label") || "Webhook URL"}
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://outlook.office.com/webhook/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <Alert>
                <InfoIcon className="mr-2 h-4 w-4" />
                <AlertTitle className="font-bold">
                  {t("notifications.form.teams.setup_webhook_title") || "Setup Microsoft Teams Webhook"}
                </AlertTitle>
                <AlertDescription>
                  <ul className="list-inside list-disc text-sm mt-2">
                    <li>
                      {t("notifications.form.teams.setup_webhook_description_1") ||
                        "Go to your Microsoft Teams channel and click on the three dots (⋯) next to the channel name"}
                    </li>
                    <li>
                      {t("notifications.form.teams.setup_webhook_description_2") ||
                        "Select 'Connectors' → 'Incoming Webhook' → 'Configure' → Copy the webhook URL"}
                    </li>
                  </ul>
                </AlertDescription>
              </Alert>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.teams.server_url_label") || "Server URL (Optional)"}
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://peekaping.example.com"
                type="url"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.teams.server_url_description") ||
                "The base URL of your Peekaping instance. Used for links in notifications. If not provided, CLIENT_URL from server config will be used."}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}

