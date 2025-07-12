import { z } from "zod";
import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { useEffect, useState } from "react";
import { Card, CardContent } from "@/components/ui/card";
import HttpOptions from "../http/options";
import Authentication from "../http/authentication";
import { Separator } from "@radix-ui/react-separator";
import Advanced from "../http/advanced";
import Intervals, {
  intervalsDefaultValues,
  intervalsSchema,
} from "../shared/intervals";
import General, {
  generalDefaultValues,
  generalSchema,
} from "../shared/general";
import Notifications, {
  notificationsDefaultValues,
  notificationsSchema,
} from "../shared/notifications";
import Proxies, {
  proxiesDefaultValues,
  proxiesSchema,
} from "../shared/proxies";
import Tags, {
  tagsDefaultValues,
  tagsSchema,
} from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { Form } from "@/components/ui/form";
import { Loader2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";

export const pushSchema = z
  .object({
    type: z.literal("push"),
    pushToken: z.string(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(proxiesSchema)
  .merge(tagsSchema);

export type PushForm = z.infer<typeof pushSchema>;
export const pushDefaultValues: PushForm = {
  type: "push",
  pushToken: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...proxiesDefaultValues,
  ...tagsDefaultValues,
};

export interface PushConfig {
  pushToken: string;
}

export const deserialize = (data: MonitorMonitorResponseDto): PushForm => {
  let config: Partial<PushConfig> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse push monitor config:", error);
    config = {};
  }

  return {
    type: "push",
    name: data.name || "My Monitor",
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    proxy_id: data.proxy_id || "",
    pushToken: data.push_token || config.pushToken || "", // Get from API or config
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: PushForm): MonitorCreateUpdateDto => {
  const config: PushConfig = {
    pushToken: formData.pushToken,
  };

  return {
    type: "push",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    proxy_id: formData.proxy_id,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
    push_token: formData.pushToken,
    tag_ids: formData.tag_ids,
  };
};

// Generate a random 24-character alphanumeric token
const generateToken = () =>
  Array.from({ length: 24 }, () => Math.random().toString(36)[2] || "x").join(
    ""
  );

const PushForm = ({
  baseUrl = "http://localhost:8034",
}: {
  baseUrl?: string;
}) => {
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
  const pushToken = form.watch("pushToken");

  useEffect(() => {
    if (!pushToken) {
      const newToken = generateToken();
      form.setValue("pushToken", newToken, { shouldDirty: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);


  const [copied, setCopied] = useState(false);
  const pushUrl = `${baseUrl}/api/v1/push/${pushToken}?status=up&msg=OK&ping=`;

  const handleCopy = () => {
    navigator.clipboard.writeText(pushUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const handleRegenerate = () => {
    const newToken = generateToken();
    form.setValue("pushToken", newToken, { shouldDirty: true });
    setCopied(false);
  };

  const onSubmit = (data: PushForm) => {
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
        path: { id: monitorId! },
        body: {
          ...payload,
          active: monitor?.data?.active,
        },
      });
    }
  };

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as PushForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>Push Monitor URL</TypographyH4>
            <div className="text-muted-foreground mb-2">
              Use this URL in your service to report status. Make an HTTP
              request (POST/GET) to this endpoint to send a heartbeat.
            </div>
            <div className="flex items-center gap-2 bg-muted rounded px-3 py-2">
              <span className="break-all font-mono text-sm">{pushUrl}</span>
            </div>
            <div className="flex gap-2 mt-2">
              <Button
                size="sm"
                variant="outline"
                type="button"
                onClick={handleCopy}
              >
                {copied ? "Copied!" : "Copy"}
              </Button>
              <Button
                size="sm"
                variant="secondary"
                type="button"
                onClick={handleRegenerate}
              >
                Regenerate
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Tags />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
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

export default PushForm;
