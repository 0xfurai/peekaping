import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { PasswordInput } from "@/components/ui/password-input";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Plus, Trash2 } from "lucide-react";
import { useFieldArray } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const RabbitMQForm = () => {
  const { t } = useLocalizedTranslation();
  const { form, setNotifierSheetOpen } = useMonitorFormContext();

  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: "nodes",
  });

  const addNode = () => {
    append({ url: "https://" });
  };

  const removeNode = (index: number) => {
    if (fields.length > 1) {
      remove(index);
    }
  };

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.rabbitmq.title")}</TypographyH4>

          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h5 className="text-sm font-medium">{t("monitors.form.rabbitmq.management_nodes_title")}</h5>
                <p className="text-xs text-muted-foreground">
                  {t("monitors.form.rabbitmq.management_nodes_description")}
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addNode}
              >
                <Plus className="h-4 w-4 mr-1" />
                Add Node
              </Button>
            </div>

            {fields.map((field, index) => (
              <div key={field.id} className="flex items-end gap-2">
                <FormField
                  control={form.control}
                  name={`nodes.${index}.url`}
                  render={({ field }) => (
                    <FormItem className="flex-1">
                      <FormLabel className={index > 0 ? "sr-only" : ""}>
                        {t("monitors.form.rabbitmq.node_url_label")} {index + 1}
                      </FormLabel>
                      <FormControl>
                        <Input
                          placeholder="https://node1.rabbitmq.com:15672"
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                {fields.length > 1 && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => removeNode(index)}
                    className="text-destructive hover:text-destructive"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                )}
              </div>
            ))}
          </div>

          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("forms.labels.username")}</FormLabel>
                <FormControl>
                  <Input placeholder="admin" {...field} />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.rabbitmq.username_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("forms.labels.password")}</FormLabel>
                <FormControl>
                  <PasswordInput
                    placeholder="password"
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.rabbitmq.password_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className="bg-muted/50 p-4 rounded-lg">
            <h6 className="text-sm font-medium mb-2">{t("monitors.form.rabbitmq.setup_information_title")}</h6>
            <p className="text-xs text-muted-foreground mb-2">
              {t("monitors.form.rabbitmq.setup_information_description")}
              <a
                href="https://www.rabbitmq.com/management.html"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary underline"
              >
                  {t("monitors.form.rabbitmq.documentation_link")}
              </a>
              .
            </p>
            <p className="text-xs text-muted-foreground">
              {t("monitors.form.rabbitmq.health_checks_description", {
                code: "<code>/api/health/checks/alarms/</code>"
              })}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Intervals />
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
    </>
  );
};

export default RabbitMQForm;
