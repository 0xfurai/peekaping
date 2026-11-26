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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const DnsForm = () => {
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
          <TypographyH4>{t("monitors.form.dns.title")}</TypographyH4>

          <FormField
            control={form.control}
            name="host"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.dns.hostname")}</FormLabel>
                <FormControl>
                  <Input placeholder="example.com" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="resolver_server"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.dns.resolver_server")}</FormLabel>
                <FormControl>
                  <Input placeholder="1.1.1.1" {...field} />
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
                <FormLabel>{t("monitors.form.dns.port")}</FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    placeholder="53"
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="resolve_type"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.dns.record_type")}</FormLabel>
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
                      <SelectValue placeholder={t("monitors.form.dns.record_type_placeholder")} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="A">A</SelectItem>
                    <SelectItem value="AAAA">AAAA</SelectItem>
                    <SelectItem value="CAA">CAA</SelectItem>
                    <SelectItem value="CNAME">CNAME</SelectItem>
                    <SelectItem value="MX">MX</SelectItem>
                    <SelectItem value="NS">NS</SelectItem>
                    <SelectItem value="PTR">PTR</SelectItem>
                    <SelectItem value="SOA">SOA</SelectItem>
                    <SelectItem value="SRV">SRV</SelectItem>
                    <SelectItem value="TXT">TXT</SelectItem>
                  </SelectContent>
                </Select>
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

export default DnsForm;
