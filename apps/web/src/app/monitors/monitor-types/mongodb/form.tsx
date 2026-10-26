import { TypographyH4 } from "@/components/ui/typography";
import { Card, CardContent } from "@/components/ui/card";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const MongoDBForm = () => {
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
        <CardContent className="pt-6">
          <TypographyH4 className="mb-4">{t("monitors.form.mongodb.configuration_title")}</TypographyH4>

          <div className="space-y-4">
            <FormField
              control={form.control}
              name="connectionString"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mongodb.connection_string_label")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="mongodb://username:password@host:27017/database"
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.mongodb.connection_string_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="command"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mongodb.command_label")}</FormLabel>
                  <FormControl>
                    <Textarea placeholder='{"ping": 1}' rows={3} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.mongodb.command_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="jsonPath"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mongodb.json_path_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="$" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.mongodb.json_path_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="expectedValue"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mongodb.expected_value_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("monitors.form.mongodb.expected_value_placeholder")} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.mongodb.expected_value_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Intervals />
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
    </>
  );
};

export default MongoDBForm;
