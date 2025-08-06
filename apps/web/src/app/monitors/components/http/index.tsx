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
import type { HttpForm } from "./schema";
import { deserialize, serialize } from "./schema";
import { useEffect } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const Http = () => {
  const { t } = useLocalizedTranslation();
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
      const parsedConfig = deserialize(monitor.data);
      form.reset(parsedConfig)
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
          {mode === "create" ? t("monitors.form.buttons.create") : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default Http;
