import { TypographyH4 } from "@/components/ui/typography";
import { Card, CardContent } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { FormField, FormItem, FormLabel, FormControl, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import { useMonitorFormContext } from "../../context/monitor-form-context";

const PostgresForm = () => {
  const { t } = useLocalizedTranslation();
  const { form, setNotifierSheetOpen } = useMonitorFormContext();

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.postgres.connection_title")}</TypographyH4>
          <FormField
            control={form.control}
            name="database_connection_string"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.postgres.connection_string_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="postgres://user:password@host:5432/database" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="database_query"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.postgres.query_label")}</FormLabel>
                <FormControl>
                  <Textarea placeholder="SELECT 1" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Tags />
          <Probe />
          <LocationRule />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Intervals />
        </CardContent>
      </Card>
    </>
  );
};

export default PostgresForm;
