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
import { Switch } from "@/components/ui/switch";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Notifications from "@/app/monitors/monitor-types/shared/notifications";
import { notificationsSchema } from "@/app/monitors/monitor-types/shared/schema";

// The messages are translation keys: FormMessage translates them.
export const createProbeSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "probes.form.name_required")
    .max(255, "probes.form.name_too_long"),
  location: z.string().trim().max(255, "probes.form.location_too_long"),
});

// Only an existing probe has channels of its own to set: a new probe starts
// with none, and sends to the channels of its monitors.
export const editProbeSchema = createProbeSchema
  .extend({ disabled: z.boolean() })
  .extend(notificationsSchema.shape);

export type CreateProbeFormValues = z.infer<typeof createProbeSchema>;
export type EditProbeFormValues = z.infer<typeof editProbeSchema>;

interface ProbeFormFieldsProps {
  // The local probe can't be disabled, so its edit form leaves the switch out.
  showDisabled?: boolean;
  // The channels are for the edit form; the create form leaves them out.
  showNotifications?: boolean;
}

// The fields of the create and edit forms. They go inside a <Form>.
const ProbeFormFields = ({
  showDisabled = false,
  showNotifications = false,
}: ProbeFormFieldsProps) => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext<EditProbeFormValues>();

  return (
    <>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("probes.form.name_label")}</FormLabel>
            <FormControl>
              <Input
                {...field}
                placeholder={t("probes.form.name_placeholder")}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="location"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("probes.form.location_label")}</FormLabel>
            <FormControl>
              <Input
                {...field}
                placeholder={t("probes.form.location_placeholder")}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      {showDisabled && (
        <FormField
          control={form.control}
          name="disabled"
          render={({ field }) => (
            <FormItem className="flex flex-row items-center justify-between gap-4 rounded-lg border p-3">
              <div className="flex flex-col gap-1">
                <FormLabel>{t("probes.form.disabled_label")}</FormLabel>
                <FormDescription>
                  {t("probes.form.disabled_description")}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
      )}
      {showNotifications && (
        <div className="flex flex-col gap-2">
          <Notifications />
          <p className="text-muted-foreground text-sm">
            {t("probes.form.notifications_hint")}
          </p>
        </div>
      )}
    </>
  );
};

export default ProbeFormFields;
