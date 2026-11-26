import { useEffect } from "react";
import { useFormContext } from "react-hook-form";
import { useQuery } from "@tanstack/react-query";
import { getWorkspaceMembersOptions } from "@/api/@tanstack/react-query.gen";
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMe } from "@/hooks/useMe";
import { recipientChoices, recipientValue } from "./recipients";

// The one field of an install email channel: which member gets the notifications. It starts on
// the signed-in user.
export default function InstallEmailForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();
  const userEmail = useMe().data?.data?.user.email;
  const members = useQuery(getWorkspaceMembersOptions()).data?.data ?? [];
  const current: string | undefined = form.watch("to");
  const value = recipientValue(current, userEmail);
  const choices = recipientChoices({ members, userEmail, current: value });

  // The select shows the user's address before anything is chosen, so the form holds it too
  useEffect(() => {
    if (!current && userEmail) form.setValue("to", userEmail);
  }, [current, userEmail, form]);

  return (
    <>
      <p className="text-sm text-muted-foreground">{t("notifications.form.install_email.description")}</p>
      <FormField
        control={form.control}
        name="to"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.install_email.to_label")}</FormLabel>
            {/* A native select, like the members page's role select */}
            <FormControl>
              <select
                data-slot="select"
                className="border-input dark:bg-input/30 h-9 w-fit max-w-full rounded-md border bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
                {...field}
                value={value}
              >
                {choices.length === 0 && (
                  <option value="">{t("notifications.form.install_email.to_placeholder")}</option>
                )}
                {choices.map((email) => (
                  <option key={email} value={email} className="bg-popover text-popover-foreground">
                    {email}
                  </option>
                ))}
              </select>
            </FormControl>
            <FormDescription>{t("notifications.form.install_email.to_description")}</FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
