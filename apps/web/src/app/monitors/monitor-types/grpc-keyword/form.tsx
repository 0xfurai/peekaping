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

const GrpcKeywordForm = () => {
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
          <FormField
            control={form.control}
            name="grpcUrl"
            render={({ field }) => (
              <FormItem>
                <FormLabel>URL</FormLabel>
                <FormControl>
                  <Input placeholder="localhost:50051" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="keyword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.grpc.keyword")}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t("monitors.form.grpc.keyword_placeholder")}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.grpc.keyword_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="invertKeyword"
            render={({ field }) => (
              <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <div className="space-y-1 leading-none">
                  <FormLabel>{t("monitors.form.grpc.invert_keyword")}</FormLabel>
                  <FormDescription>
                    {t("monitors.form.grpc.invert_keyword_description")}
                  </FormDescription>
                </div>
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.grpc.options_title")}</TypographyH4>

          <FormField
            control={form.control}
            name="grpcEnableTls"
            render={({ field }) => (
              <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                <FormControl>
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <div className="space-y-1 leading-none">
                  <FormLabel>{t("monitors.form.grpc.enable_tls")}</FormLabel>
                  <FormDescription>
                    {t("monitors.form.grpc.enable_tls_description")}
                  </FormDescription>
                </div>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="grpcServiceName"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.grpc.proto_service_name")}</FormLabel>
                <FormControl>
                  <Input placeholder={t("monitors.form.grpc.service_name_placeholder")} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="grpcMethod"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.grpc.proto_method")}</FormLabel>
                <FormControl>
                  <Input placeholder={t("monitors.form.grpc.method_placeholder")} {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.grpc.method_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="grpcProtobuf"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.grpc.proto_content")}</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder={`Example:
syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`}
                    className="min-h-[200px] font-mono text-sm"
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="grpcBody"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.grpc.body")}</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder={`Example:
{
  "key": "value"
}`}
                    className="min-h-[100px] font-mono text-sm"
                    {...field}
                  />
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

export default GrpcKeywordForm;
