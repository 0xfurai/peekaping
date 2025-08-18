import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Loader2, Plus, X } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { TypographyH4 } from "@/components/ui/typography";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import General from "../shared/general";
import Intervals from "../shared/intervals";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import {
  type KafkaProducerForm as KafkaProducerFormType,
  kafkaProducerDefaultValues,
  serialize,
} from "./schema";

import { PasswordInput } from "@/components/ui/password-input";

const KafkaProducerForm = () => {
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

  const [brokers, setBrokers] = useState<string[]>(
    () => form.getValues("brokers") || ["localhost:9092"]
  );

  const saslMechanism = form.watch("sasl_mechanism");

  const onSubmit = (data: KafkaProducerFormType) => {
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
      form.reset(kafkaProducerDefaultValues);
      setBrokers(kafkaProducerDefaultValues.brokers);
    }
  }, [mode, form]);

  const addBroker = () => {
    const newBrokers = [...brokers, "localhost:9092"];
    setBrokers(newBrokers);
    form.setValue("brokers", newBrokers);
  };

  const removeBroker = (index: number) => {
    if (brokers.length > 1) {
      const newBrokers = brokers.filter((_, i) => i !== index);
      setBrokers(newBrokers);
      form.setValue("brokers", newBrokers);
    }
  };

  const updateBroker = (index: number, value: string) => {
    const newBrokers = [...brokers];
    newBrokers[index] = value;
    setBrokers(newBrokers);
    form.setValue("brokers", newBrokers);
  };

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) =>
          onSubmit(data as KafkaProducerFormType)
        )}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.kafka.configuration_title")}</TypographyH4>

            <div className="space-y-4">
              <div className="space-y-2">
                <FormLabel>{t("monitors.form.kafka.brokers_label")}</FormLabel>
                <FormDescription>
                  {t("monitors.form.kafka.brokers_description")}
                </FormDescription>
                {brokers.map((broker, index) => (
                  <div key={index} className="flex items-center gap-2">
                    <div className="flex-1">
                      <Input
                        placeholder="localhost:9092"
                        value={broker}
                        onChange={(e) => updateBroker(index, e.target.value)}
                      />
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      onClick={() => removeBroker(index)}
                      disabled={brokers.length === 1}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={addBroker}
                  className="mt-2"
                >
                  <Plus className="h-4 w-4 mr-2" />
                  {t("monitors.form.kafka.add_broker")}
                </Button>
              </div>
            </div>

            <FormField
              control={form.control}
              name="topic"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Topic</FormLabel>
                  <FormControl>
                    <Input placeholder="test-topic" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.kafka.topic_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="message"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.kafka.message_label")}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder='{"status": "up", "timestamp": "2024-01-01T00:00:00Z"}'
                      className="font-mono text-sm"
                      rows={4}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.kafka.message_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="allow_auto_topic_creation"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>{t("monitors.form.kafka.auto_topic_creation_label")}</FormLabel>
                    <FormDescription>
                      {t("monitors.form.kafka.auto_topic_creation_description")}
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.kafka.security_title")}</TypographyH4>

            <FormField
              control={form.control}
              name="ssl"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>{t("monitors.form.kafka.enable_ssl_label")}</FormLabel>
                    <FormDescription>
                      {t("monitors.form.kafka.enable_ssl_description")}
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="sasl_mechanism"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.kafka.sasl_mechanism_label")}</FormLabel>
                  <Select
                    onValueChange={(val) => {
                      if (!val) {
                        return;
                      }
                      field.onChange(val);
                    }}
                    value={field.value}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t("monitors.form.kafka.sasl_mechanism_placeholder")} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="None">None</SelectItem>
                      <SelectItem value="PLAIN">PLAIN</SelectItem>
                      <SelectItem value="SCRAM-SHA-256">
                        SCRAM-SHA-256
                      </SelectItem>
                      <SelectItem value="SCRAM-SHA-512">
                        SCRAM-SHA-512
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t("monitors.form.kafka.sasl_mechanism_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            {saslMechanism !== "None" && (
              <>
                <FormField
                  control={form.control}
                  name="sasl_username"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t("monitors.form.kafka.sasl_username_label")}</FormLabel>
                      <FormControl>
                        <Input placeholder="kafka_user" {...field} />
                      </FormControl>
                      <FormDescription>
                        {t("monitors.form.kafka.sasl_username_description")}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="sasl_password"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t("monitors.form.kafka.sasl_password_label")}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          placeholder={t("monitors.form.kafka.sasl_password_placeholder")}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t("monitors.form.kafka.sasl_password_description")}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}
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
          </CardContent>
        </Card>

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? t("monitors.form.buttons.create") : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default KafkaProducerForm;
