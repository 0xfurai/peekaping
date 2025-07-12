import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import Advanced from "./advanced";
import Authentication from "./authentication";
import HttpOptions from "./options";
import { Separator } from "@/components/ui/separator";
import { Card, CardContent } from "@/components/ui/card";
import Notifications from "../shared/notifications";
import Proxies from "../shared/proxies";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Tags from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import type { HttpExecutorConfig, HttpForm } from "./schema";
import { serialize } from "./schema";
import type { HttpOptionsForm } from "./options";
import type { AuthenticationForm } from "./authentication";
import { useEffect } from "react";

const Http = () => {
  const {
    form,
    setNotifierSheetOpen,
    setProxySheetOpen,
    isPending,
    mode,
    createMonitorMutation,
    editMonitorMutation,
    monitorId,
    monitor,
  } = useMonitorFormContext();

  const onSubmit = (data: HttpForm) => {
    const payload = serialize(data);

    if (mode === "create") {
      createMonitorMutation.mutate({
        body: {
          ...payload,
          active: true,
        },
      });
    } else {
      editMonitorMutation.mutate({
        path: {
          id: monitorId!,
        },
        body: {
          ...payload,
          active: monitor?.data?.active,
        },
      });
    }
  };

  // Reset form with monitor data in edit mode
  useEffect(() => {
    if (mode === "edit" && monitor?.data) {
      // const monitorType = (monitor.data.type ?? "http") as "http" | "push";
      const { config } = monitor.data;
      const parsedConfig: HttpExecutorConfig = config ? JSON.parse(config) : {};

      // Ensure httpOptions matches the discriminated union structure
      const httpOptions: HttpOptionsForm = {
        method: parsedConfig.method || "GET",
        encoding: parsedConfig.encoding || "json",
        headers: parsedConfig.headers || '{ "Content-Type": "application/json" }',
        body: parsedConfig.body || "",
      } as HttpOptionsForm;

      // Construct authentication object based on authMethod
      let authentication: AuthenticationForm;
      switch (parsedConfig.authMethod) {
        case "basic":
          authentication = {
            authMethod: "basic",
            basic_auth_user: parsedConfig.basic_auth_user || "",
            basic_auth_pass: parsedConfig.basic_auth_pass || "",
          };
          break;
        case "oauth2-cc":
          authentication = {
            authMethod: "oauth2-cc",
            oauth_auth_method: (parsedConfig.oauth_auth_method as "client_secret_basic" | "client_secret_post") || "client_secret_basic",
            oauth_token_url: parsedConfig.oauth_token_url || "",
            oauth_client_id: parsedConfig.oauth_client_id || "",
            oauth_client_secret: parsedConfig.oauth_client_secret || "",
            oauth_scopes: parsedConfig.oauth_scopes,
          };
          break;
        case "ntlm":
          authentication = {
            authMethod: "ntlm",
            basic_auth_user: parsedConfig.basic_auth_user || "",
            basic_auth_pass: parsedConfig.basic_auth_pass || "",
            authDomain: parsedConfig.authDomain || "",
            authWorkstation: parsedConfig.authWorkstation || "",
          };
          break;
        case "mtls":
          authentication = {
            authMethod: "mtls",
            tlsCert: parsedConfig.tlsCert || "",
            tlsKey: parsedConfig.tlsKey || "",
            tlsCa: parsedConfig.tlsCa || "",
          };
          break;
        default:
          authentication = {
            authMethod: "none",
          };
      }

      form.reset({
        type: "http",
        name: monitor.data.name,
        url: parsedConfig.url,
        interval: monitor.data.interval,
        max_retries: monitor.data.max_retries,
        retry_interval: monitor.data.retry_interval,
        timeout: monitor.data.timeout,
        resend_interval: monitor.data.resend_interval,
        notification_ids: monitor.data.notification_ids,
        tag_ids: monitor.data.tag_ids,
        proxy_id: monitor.data.proxy_id,
        accepted_statuscodes: parsedConfig.accepted_statuscodes,
        max_redirects: parsedConfig.max_redirects,
        ignore_tls_errors: parsedConfig.ignore_tls_errors || false,
        httpOptions,
        authentication,
      });
    }
  }, [form, monitor, mode]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as HttpForm))}
        className="space-y-6 max-w-[600px]"
      >
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
            <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Tags />
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

        <Button type="submit">
          {isPending && <Loader2 className="animate-spin" />}
          {mode === "create" ? "Create" : "Update"}
        </Button>
      </form>
    </Form>
  );
};

export default Http;
