import { TypographyH4 } from "@/components/ui/typography";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const RedisForm = () => {
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
          <TypographyH4>{t("monitors.form.redis.title")}</TypographyH4>

          <FormField
            control={form.control}
            name="databaseConnectionString"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.redis.connection_string_label")}</FormLabel>
                <FormControl>
                  <Input
                    placeholder="redis://user:password@host:port"
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.redis.connection_string_description", {
                    redis: "redis://[user:password@]host[:port][/db]",
                    rediss: "rediss://[user:password@]host[:port][/db]",
                  })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="ignoreTls"
            render={({ field }) => (
              <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <div className="space-y-1 leading-none">
                  <FormLabel>{t("monitors.form.redis.ignore_tls_label")}</FormLabel>
                  <FormDescription>
                    {t("monitors.form.redis.ignore_tls_description")}
                  </FormDescription>
                </div>
              </FormItem>
            )}
          />

          {form.watch("databaseConnectionString")?.startsWith("rediss://") && (
            <>
              <FormField
                control={form.control}
                name="caCert"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.redis.ca_certificate_label")}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                        className="font-mono text-sm"
                        rows={6}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t("monitors.form.redis.ca_certificate_description")}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="clientCert"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.redis.client_certificate_label")}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                        className="font-mono text-sm"
                        rows={6}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t("monitors.form.redis.client_certificate_description")}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="clientKey"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.redis.client_key_label")}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                        className="font-mono text-sm"
                        rows={6}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t("monitors.form.redis.client_key_description")}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </>
          )}
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

export default RedisForm;
