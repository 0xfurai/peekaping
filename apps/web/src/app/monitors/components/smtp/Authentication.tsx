import { TypographyH4 } from "@/components/ui/typography";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const Authentication = () => {
  const { t } = useLocalizedTranslation();
  const { form } = useMonitorFormContext();

  return (
    <>
      <TypographyH4>{t("monitors.form.smtp.authentication_title")}</TypographyH4>

      <FormField
        control={form.control}
        name="username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.username")}</FormLabel>
            <FormControl>
              <Input placeholder="user@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("monitors.form.smtp.username_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="password"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.password")}</FormLabel>
            <FormControl>
              <Input type="password" placeholder="••••••••" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default Authentication;
