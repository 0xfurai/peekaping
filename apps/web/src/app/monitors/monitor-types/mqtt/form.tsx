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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { PasswordInput } from "@/components/ui/password-input";

const MqttForm = () => {
  const { t } = useLocalizedTranslation();
  const { form, setNotifierSheetOpen } = useMonitorFormContext();

  const checkType = form.watch("check_type");

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.mqtt.configuration_title")}</TypographyH4>
          <FormField
            control={form.control}
            name="hostname"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mqtt.hostname_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="localhost" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="port"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mqtt.port_label")}</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    min="1"
                    max="65535"
                    placeholder="1883"
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="topic"
            render={({ field }) => (
              <FormItem>
                <FormLabel>MQTT Topic</FormLabel>
                <FormControl>
                  <Input placeholder="test/topic" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mqtt.username_label")}</FormLabel>
                <FormControl>
                  <Input placeholder={t("monitors.form.mqtt.username_placeholder")} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mqtt.password_label")}</FormLabel>
                <FormControl>
                  <PasswordInput
                    placeholder={t("monitors.form.mqtt.password_placeholder")}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="check_type"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.mqtt.check_type_label")}</FormLabel>
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder={t("monitors.form.mqtt.check_type_placeholder")} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="none">{t("monitors.form.mqtt.check_type_none")}</SelectItem>
                    <SelectItem value="keyword">{t("monitors.form.mqtt.check_type_keyword")}</SelectItem>
                    <SelectItem value="json-query">{t("monitors.form.mqtt.check_type_json_query")}</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          {checkType === "keyword" && (
            <FormField
              control={form.control}
              name="success_keyword"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mqtt.success_keyword_label")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t("monitors.form.mqtt.success_keyword_placeholder")}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          {checkType === "json-query" && (
            <>
              <FormField
                control={form.control}
                name="json_path"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.mqtt.json_path_label")}</FormLabel>
                    <FormControl>
                      <Input placeholder="$.status" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="expected_value"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.mqtt.expected_value_label")}</FormLabel>
                    <FormControl>
                      <Input placeholder="ok" {...field} />
                    </FormControl>
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

export default MqttForm;
