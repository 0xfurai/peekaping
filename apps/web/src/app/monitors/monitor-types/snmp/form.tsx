import { TypographyH4 } from "@/components/ui/typography";
import { Card, CardContent } from "@/components/ui/card";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const SnmpForm = () => {
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
          <TypographyH4>{t("monitors.form.snmp.title")}</TypographyH4>

          <FormField
            control={form.control}
            name="host"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.host_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="127.0.0.1" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.host_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="port"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.port_label")}</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    placeholder="161"
                    min="1"
                    max="65535"
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.port_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="community"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.community_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="public" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.community_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="snmp_version"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.snmp_version_label")}</FormLabel>
                <Select
                  onValueChange={val => {
                    if (!val) {
                      return;
                    }
                    field.onChange(val);
                  }}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder="Select SNMP version" />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="v1">SNMPv1</SelectItem>
                    <SelectItem value="v2c">SNMPv2c</SelectItem>
                    <SelectItem value="v3">SNMPv3</SelectItem>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t("monitors.form.snmp.snmp_version_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="oid"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.oid_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="1.3.6.1.2.1.1.1.0" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.oid_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.snmp.value_validation_label")}</TypographyH4>

          <FormField
            control={form.control}
            name="json_path"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.json_path_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="$" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.json_path_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="json_path_operator"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.json_path_operator_label")}</FormLabel>
                <Select
                  onValueChange={(val) => {
                    if (!val) {
                      return;
                    }
                    field.onChange(val);
                  }}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder="Select condition" />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="eq">== ({t("monitors.form.snmp.json_path_operator_eq")})</SelectItem>
                    <SelectItem value="ne">!= ({t("monitors.form.snmp.json_path_operator_ne")})</SelectItem>
                    <SelectItem value="lt">&lt; ({t("monitors.form.snmp.json_path_operator_lt")})</SelectItem>
                    <SelectItem value="gt">&gt; ({t("monitors.form.snmp.json_path_operator_gt")})</SelectItem>
                    <SelectItem value="le">
                      &le; ({t("monitors.form.snmp.json_path_operator_le")})
                    </SelectItem>
                    <SelectItem value="ge">
                      &ge; ({t("monitors.form.snmp.json_path_operator_ge")})
                    </SelectItem>
                  </SelectContent>
                </Select>
                <FormDescription>
                  {t("monitors.form.snmp.json_path_operator_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="expected_value"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.snmp.expected_value_label")}</FormLabel>
                <FormControl>
                  <Input placeholder="Expected value" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.snmp.expected_value_description")}
                </FormDescription>
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

export default SnmpForm;
