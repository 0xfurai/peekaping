import { FormField, FormItem, FormLabel, FormControl, FormMessage, FormDescription } from "@/components/ui/form";
import { SecretInput } from "../../components/saved-secrets";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export default function GoogleChatForm() {
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
              {t("notifications.form.google_chat.webhook_url_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <SecretInput
                placeholder="https://chat.googleapis.com/v1/spaces/..."
                autoComplete="off"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.google_chat.more_info_about_webhooks")}:{" "}
                <a
                  href="https://developers.google.com/chat/how-tos/webhooks"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://developers.google.com/chat/how-tos/webhooks
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
