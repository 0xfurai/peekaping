import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import Advanced from "../http/advanced";
import Authentication from "../http/authentication";
import HttpOptions from "../http/options";
import { Separator } from "@/components/ui/separator";
import { Card, CardContent } from "@/components/ui/card";
import Notifications from "../shared/notifications";
import Proxies from "../shared/proxies";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const HttpJsonQueryForm = () => {
  const { t } = useLocalizedTranslation();
  const { form, setNotifierSheetOpen, setProxySheetOpen } = useMonitorFormContext();

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <FormField
            control={form.control}
            name="url"
            render={({ field }) => (
              <FormItem>
                <FormLabel>URL</FormLabel>
                <FormControl>
                  <Input placeholder="https://" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <h4 className="text-lg font-semibold">{t("monitors.form.http_json_query.title")}</h4>
          <div className="text-sm text-muted-foreground mb-4">
            Parse and extract specific data from the server's JSON response using GJSON path syntax.
            <br /><br />
            <strong>Leave empty to compare the entire JSON response:</strong> When no query is specified, the system will perform deep JSON equality comparison, ignoring key ordering and whitespace differences. This ensures accurate structural comparison of complete JSON objects.
            <br /><br />
            <strong>With query:</strong> Extract specific values and compare as strings using the specified condition.
            <br /><br />
            See <a href="https://github.com/tidwall/gjson/blob/master/SYNTAX.md" target="_blank" rel="noopener noreferrer" className="underline">GJSON syntax documentation</a> for path examples and supported features.
          </div>

          <FormField
            control={form.control}
            name="json_query"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.http_json_query.json_query_label")}</FormLabel>
                <FormControl>
                  <Input
                    placeholder="e.g., user.name or items.0.id (leave empty for full response)"
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className="grid grid-cols-2 gap-4">
            <FormField
              control={form.control}
              name="json_condition"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.http_json_query.condition_label")}</FormLabel>
                  <Select onValueChange={field.onChange} defaultValue={field.value}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select condition" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="==">==</SelectItem>
                      <SelectItem value="!=">!=</SelectItem>
                      <SelectItem value=">">&gt;</SelectItem>
                      <SelectItem value="<">&lt;</SelectItem>
                      <SelectItem value=">=">&gt;=</SelectItem>
                      <SelectItem value="<=">&lt;=</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="expected_value"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.http_json_query.expected_value_label")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="Expected value (full JSON if no query specified)"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
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
          <Proxies onNewProxy={() => setProxySheetOpen(true)} />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Intervals />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Advanced />
          <Separator className="my-8" />
          <Authentication />
          <Separator className="my-8" />
          <HttpOptions />
        </CardContent>
      </Card>
    </>
  );
};

export default HttpJsonQueryForm;
