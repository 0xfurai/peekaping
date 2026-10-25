import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
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

const HttpKeywordForm = () => {
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
          <h4 className="text-lg font-semibold">{t("monitors.form.http_keyword.keyword_validation_title")}</h4>

          <FormField
            control={form.control}
            name="keyword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.http_keyword.keyword_label")}</FormLabel>
                <FormControl>
                  <Input
                    placeholder="Search keyword in plain HTML or JSON response. The search is case-sensitive."
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="invert_keyword"
            render={({ field }) => (
              <FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
                <div className="space-y-0.5">
                  <FormLabel>{t("monitors.form.http_keyword.invert_keyword_label")}</FormLabel>
                  <div className="text-sm text-muted-foreground">
                    {t("monitors.form.http_keyword.invert_keyword_description")}
                  </div>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
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

export default HttpKeywordForm;
