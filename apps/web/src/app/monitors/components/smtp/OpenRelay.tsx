import { TypographyH4 } from "@/components/ui/typography";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from "@/components/ui/form";
import { Checkbox } from "@/components/ui/checkbox";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const OpenRelay = () => {
  const { t } = useLocalizedTranslation();
  const { form } = useMonitorFormContext();

  const testOpenRelay = form.watch("test_open_relay");

  return (
    <>
      <TypographyH4>{t("monitors.form.smtp.open_relay_title")}</TypographyH4>

      <FormField
        control={form.control}
        name="test_open_relay"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                onCheckedChange={field.onChange}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>{t("monitors.form.smtp.test_open_relay")}</FormLabel>
              <FormDescription>
                {t("monitors.form.smtp.test_open_relay_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />

      {testOpenRelay && (
        <FormField
          control={form.control}
          name="expect_secure_relay"
          render={({ field }) => (
            <FormItem className="flex flex-row items-start space-x-3 space-y-0">
              <FormControl>
                <Checkbox
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <div className="space-y-1 leading-none">
                <FormLabel>{t("monitors.form.smtp.expect_secure_relay")}</FormLabel>
                <FormDescription>
                  {t("monitors.form.smtp.expect_secure_relay_description")}
                </FormDescription>
              </div>
            </FormItem>
          )}
        />
      )}
    </>
  );
};

export default OpenRelay;
