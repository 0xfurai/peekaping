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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useWatch } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const TLSSection = () => {
  const { t } = useLocalizedTranslation();
  const { form } = useMonitorFormContext();
  const connectionType = useWatch({
    control: form.control,
    name: "connection_type",
  });
  const tlsEnabled = useWatch({
    control: form.control,
    name: "tls_enabled",
  });

  // Don't show TLS section for socket connections
  if (connectionType !== "tcp") {
    return null;
  }

  return (
    <Card>
      <CardContent className="space-y-4">
        <TypographyH4>{t("monitors.form.docker.tls_configuration")}</TypographyH4>

        <FormField
          control={form.control}
          name="tls_enabled"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("monitors.form.docker.enable_tls")}</FormLabel>
              <Select
                onValueChange={(val) => {
                  field.onChange(val === "true");
                }}
                value={field.value ? "true" : "false"}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder={t("monitors.form.docker.select_tls_option")} />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="false">{t("monitors.form.docker.disabled")}</SelectItem>
                  <SelectItem value="true">{t("monitors.form.docker.enabled")}</SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        {tlsEnabled && (
          <>
            <FormField
              control={form.control}
              name="tls_verify"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.docker.verify_tls")}</FormLabel>
                  <Select
                    onValueChange={(val) => {
                      field.onChange(val === "true");
                    }}
                    value={field.value ? "true" : "false"}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t("monitors.form.docker.select_verification_option")} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="true">{t("monitors.form.docker.verify_certificates")}</SelectItem>
                      <SelectItem value="false">{t("monitors.form.docker.skip_verification")}</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_cert"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.docker.client_certificate")}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    {t("monitors.form.docker.client_cert_description")}
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_key"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.docker.client_private_key")}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    {t("monitors.form.docker.private_key_description")}
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_ca"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.docker.ca_certificate")}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    {t("monitors.form.docker.ca_cert_description")}
                  </div>
                </FormItem>
              )}
            />

            <div className="bg-amber-50 border border-amber-200 rounded-md p-4">
              <div className="text-sm text-amber-800">
                <strong>{t("monitors.form.docker.note")}</strong> {t("monitors.form.docker.mtls_note")}
              </div>
            </div>

            <div className="bg-blue-50 border border-blue-200 rounded-md p-4">
              <div className="text-sm text-blue-800">
                <strong>{t("monitors.form.docker.common_tls_issues")}</strong>
                <ul className="mt-2 space-y-1 list-disc list-inside">
                  <li><strong>{t("monitors.form.docker.legacy_cert_error")}</strong> {t("monitors.form.docker.legacy_cert_solution")}</li>
                  <li><strong>{t("monitors.form.docker.unknown_authority")}</strong> {t("monitors.form.docker.unknown_authority_solution")}</li>
                  <li><strong>{t("monitors.form.docker.hostname_mismatch")}</strong> {t("monitors.form.docker.hostname_mismatch_solution")}</li>
                </ul>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
};

const DockerForm = () => {
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
          <TypographyH4>{t("monitors.form.docker.docker_container")}</TypographyH4>
          <FormField
            control={form.control}
            name="container_id"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.docker.container_name_id")}</FormLabel>
                <FormControl>
                  <Input placeholder="my-container" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.docker.docker_host")}</TypographyH4>
          <FormField
            control={form.control}
            name="connection_type"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("monitors.form.docker.connection_type")}</FormLabel>
                <Select
                  onValueChange={(val) => {
                    if (!val) {
                      return;
                    }
                    field.onChange(val);
                    // Reset TLS settings when switching from TCP to socket
                    if (val === "socket") {
                      form.setValue("tls_enabled", false);
                      form.setValue("docker_daemon", "/var/run/docker.sock");
                    } else if (val === "tcp") {
                      form.setValue("docker_daemon", "http://localhost:2375");
                    }
                  }}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder={t("monitors.form.docker.select_connection_type")} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="socket">Socket</SelectItem>
                    <SelectItem value="tcp">TCP/HTTP</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="docker_daemon"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Docker Daemon</FormLabel>
                <FormControl>
                  <Input
                    placeholder="/var/run/docker.sock or http://host:2375"
                    {...field}
                  />
                </FormControl>
                <FormMessage />
                <div className="text-sm text-muted-foreground">
                  <p className="font-medium mb-1">{t("monitors.form.docker.examples")}:</p>
                  <ul className="list-disc list-inside space-y-0.5">
                    <li>/var/run/docker.sock</li>
                    <li>http://localhost:2375</li>
                    <li>https://localhost:2376 (TLS)</li>
                  </ul>
                </div>
              </FormItem>
            )}
          />
        </CardContent>
      </Card>

      <TLSSection />

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

export default DockerForm;
