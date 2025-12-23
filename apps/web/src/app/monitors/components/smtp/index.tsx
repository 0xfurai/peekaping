import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  Form,
} from "@/components/ui/form";
import { Loader2 } from "lucide-react";
import { useEffect } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { serialize, smtpDefaultValues, type SMTPForm } from "./schema";
import Tags from "../shared/tags";
import General from "../shared/general";
import Intervals from "../shared/intervals";
import Notifications from "../shared/notifications";
import SMTPSettings from "./SMTPSettings";
import Authentication from "./Authentication";
import OpenRelay from "./OpenRelay";

const SMTP = () => {
  const { t } = useLocalizedTranslation();
  const {
    form,
    setNotifierSheetOpen,
    isPending,
    mode,
    createMonitorMutation,
    editMonitorMutation,
    monitorId,
    monitor,
  } = useMonitorFormContext();

  const onSubmit = (data: SMTPForm) => {
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

  useEffect(() => {
    if (mode === "create") {
      // Preserve the current name when resetting form
      const currentName = form.getValues("name");
      form.reset({
        ...smtpDefaultValues,
        name: currentName || smtpDefaultValues.name,
      });
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as SMTPForm))}
        className="space-y-6 max-w-[600px]"
      >
        <fieldset disabled={isPending} className="space-y-6">
          <Card>
            <CardContent className="space-y-4">
              <General />
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-4">
              <SMTPSettings />
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-4">
              <Authentication />
            </CardContent>
          </Card>

          <Card>
            <CardContent className="space-y-4">
              <OpenRelay />
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
              <Intervals />
            </CardContent>
          </Card>
        </fieldset>

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="animate-spin mr-2" />}
          {mode === "create" ? t("monitors.form.buttons.create") : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default SMTP;

