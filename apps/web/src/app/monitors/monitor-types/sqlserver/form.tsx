import { TypographyH4 } from "@/components/ui/typography";
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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const SqlServerForm = () => {
  const { form, setNotifierSheetOpen } = useMonitorFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.sqlserver.title")}</TypographyH4>
          <FormField
            control={form.control}
            name="database_connection_string"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.sqlserver.connection_string_label")}</FormLabel>
                <FormControl>
                  <Input
                    placeholder="Server=localhost,1433;Database=mydb;User Id=sa;Password=..."
                    {...field}
                  />
                </FormControl>
                <div className="text-sm text-muted-foreground space-y-3">
                  <div>
                    <p className="font-medium mb-2">Connection string format:</p>
                    <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                      Server=&lt;hostname&gt;,&lt;port&gt;;Database=&lt;database&gt;;User Id=&lt;username&gt;;Password=&lt;password&gt;;Encrypt=&lt;true/false&gt;;TrustServerCertificate=&lt;true/false&gt;;Connection Timeout=&lt;seconds&gt;
                    </code>
                  </div>
                </div>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="database_query"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.sqlserver.query_label")}</FormLabel>
                <FormControl>
                  <Textarea placeholder="SELECT 1" {...field} />
                </FormControl>
                <div className="text-sm text-muted-foreground">
                  <p>
                    {t("monitors.form.sqlserver.query_description")}
                    <code className="text-xs bg-muted px-1 py-0.5 rounded">SELECT 1</code>.
                  </p>
                  <p className="mt-1">
                    <span className="font-medium">{t("monitors.form.sqlserver.allowed_statements_label")}</span> {t("monitors.form.sqlserver.allowed_statements_description")}
                  </p>
                </div>
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

export default SqlServerForm;
