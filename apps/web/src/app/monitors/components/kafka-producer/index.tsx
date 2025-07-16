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

const KafkaProducerForm = () => {
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
            <TypographyH4>Kafka Configuration</TypographyH4>

            <div className="space-y-4">
              <div className="space-y-2">
                <FormLabel>Kafka Brokers</FormLabel>
                <FormDescription>
                  List of Kafka broker addresses (host:port format)
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
                  Add Broker
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
                    The Kafka topic to produce messages to
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
                  <FormLabel>Message</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder='{"status": "up", "timestamp": "2024-01-01T00:00:00Z"}'
                      className="font-mono text-sm"
                      rows={4}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    The message content to send to the topic (JSON format
                    recommended)
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
                    <FormLabel>Allow Auto Topic Creation</FormLabel>
                    <FormDescription>
                      Allow the producer to automatically create the topic if it
                      doesn't exist
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>Security</TypographyH4>

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
                    <FormLabel>Enable SSL/TLS</FormLabel>
                    <FormDescription>
                      Use SSL/TLS encryption for the connection
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
                  <FormLabel>SASL Mechanism</FormLabel>
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
                        <SelectValue placeholder="Select SASL mechanism" />
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
                    Choose the SASL authentication mechanism
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
                      <FormLabel>SASL Username</FormLabel>
                      <FormControl>
                        <Input placeholder="kafka_user" {...field} />
                      </FormControl>
                      <FormDescription>
                        Username for SASL authentication
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
                      <FormLabel>SASL Password</FormLabel>
                      <FormControl>
                        <Input
                          type="password"
                          placeholder="password"
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        Password for SASL authentication
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
          {mode === "create" ? "Create Monitor" : "Update Monitor"}
        </Button>
      </form>
    </Form>
  );
};

export default KafkaProducerForm;
