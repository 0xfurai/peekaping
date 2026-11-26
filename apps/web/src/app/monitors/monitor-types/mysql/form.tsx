import { TypographyH4 } from "@/components/ui/typography";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { Card, CardContent } from "@/components/ui/card";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

const MySQLForm = () => {
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
          <TypographyH4>{t("monitors.form.mysql.connection_title")}</TypographyH4>
          <FormField
            control={form.control}
            name="connection_string"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mysql.connection_string_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="mysql://username:password@host:3306/database" {...field} />
                </FormControl>
                <div className="text-sm text-muted-foreground">
                  {t("monitors.form.mysql.connection_string_examples")}
                  <ul className="list-disc list-inside mt-1 space-y-1">
                    <li><code>mysql://user:pass@localhost:3306/mydb</code></li>
                    <li><code>mysql://user:pass@host/database</code> (default port 3306)</li>
                    <li><code>mysql://user:pass@host:3306/db?charset=utf8</code></li>
                  </ul>
                </div>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="query"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mysql.query_label")}</FormLabel>
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
          <Tags />
          <Probe />
          <LocationRule />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
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

export default MySQLForm;
