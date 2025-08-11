import { MultiSelect } from "@/components/multi-select";
import {
  FormControl,
  FormField,
  FormLabel,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { FormItem } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { TypographyH4 } from "@/components/ui/typography";
import { useFormContext, useWatch } from "react-hook-form";
import { z } from "zod";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const acceptedStatusCodesOptions = [
  { value: "1XX", label: "1XX" },
  { value: "2XX", label: "2XX" },
  { value: "3XX", label: "3XX" },
  { value: "4XX", label: "4XX" },
  { value: "5XX", label: "5XX" },
];

export const advancedSchema = z.object({
  accepted_statuscodes: z.array(z.string()),
  max_redirects: z.coerce.number().min(0).max(30),
  ignore_tls_errors: z.boolean(),
  check_cert_expiry: z.boolean(),
});

export type AdvancedForm = z.infer<typeof advancedSchema>;

export const advancedDefaultValues: AdvancedForm = {
  accepted_statuscodes: ["2XX"],
  max_redirects: 10,
  ignore_tls_errors: false,
  check_cert_expiry: false,
}

const Advanced = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  // Watch the monitor type to conditionally show certificate expiry field
  const monitorType = useWatch({
    control: form.control,
    name: "type",
  });

  return (
    <>
      <TypographyH4>{t("monitors.form.http.advanced.title")}</TypographyH4>

      <FormField
        control={form.control}
        name="accepted_statuscodes"
        render={({ field }) => {
          return <FormItem>
          <FormLabel>{t("monitors.form.http.advanced.accepted_status_codes")}</FormLabel>
          <FormControl>
            <MultiSelect
              options={acceptedStatusCodesOptions}
              onValueChange={(val) => {
                field.onChange(val)
              }}
              value={field.value || []}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
        }}
      />

      <FormField
        control={form.control}
        name="max_redirects"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.advanced.max_redirects")}</FormLabel>
            <FormControl>
              <Input placeholder="10" {...field} type="number" />
            </FormControl>
            <FormDescription>
              {t("monitors.form.http.advanced.max_redirects_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="ignore_tls_errors"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>
                {t("monitors.form.http.advanced.ignore_tls")}
              </FormLabel>
              <FormDescription>
                {t("monitors.form.http.advanced.ignore_tls_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />

      {/* Only show certificate expiry field for HTTP monitors */}
      {monitorType === "http" && (
        <FormField
          control={form.control}
          name="check_cert_expiry"
          render={({ field }) => (
            <FormItem className="flex flex-row items-start space-x-3 space-y-0">
              <FormControl>
                <Checkbox
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <div className="space-y-1 leading-none">
                <FormLabel>
                  Check certificate expiry
                </FormLabel>
                <FormDescription>
                  Monitor SSL/TLS certificate expiration and send notifications when certificates are about to expire.
                </FormDescription>
              </div>
            </FormItem>
          )}
        />
      )}
    </>
  );
};

export default Advanced;
