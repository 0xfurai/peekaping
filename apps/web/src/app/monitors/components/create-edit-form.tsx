import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Form } from "@/components/ui/form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMonitorFormContext } from "../context/monitor-form-context";
import {
  getMonitorType,
  serializeMonitor,
  type MonitorFormValues,
} from "../monitor-types/registry";

// The monitor form: the form element, the submit button and the chosen type's fields. The type's
// definition supplies the fields and the codec; the general section's Type select switches type.
const CreateEditForm = () => {
  const { t } = useLocalizedTranslation();
  const {
    form,
    mode,
    isPending,
    createMonitorMutation,
    editMonitorMutation,
    monitorId,
    monitor,
  } = useMonitorFormContext();
  const type = form.watch("type");
  const definition = getMonitorType(type);

  const onSubmit = (values: MonitorFormValues) => {
    const payload = serializeMonitor(values);
    if (mode === "create") {
      createMonitorMutation.mutate({ body: { ...payload, active: true } });
    } else {
      editMonitorMutation.mutate({
        path: { id: monitorId! },
        body: { ...payload, active: monitor?.data?.active },
      });
    }
  };

  if (!definition) {
    console.log("No monitor type definition for", type);
    return null;
  }

  if (Object.keys(form.formState.errors).length > 0) {
    console.log("CreateEditFields errors", form.formState.errors);
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6 max-w-[600px]"
      >
        <definition.Form />

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="animate-spin" />}
          {mode === "create"
            ? t("monitors.form.buttons.create")
            : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default CreateEditForm;
