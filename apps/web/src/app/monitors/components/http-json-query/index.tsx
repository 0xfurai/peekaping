import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import Advanced from "../http/advanced";
import Authentication from "../http/authentication";
import HttpOptions from "../http/options";
import { Separator } from "@/components/ui/separator";
import { Card, CardContent } from "@/components/ui/card";
import { TypographyH4 } from "@/components/typography";
import Notifications from "../shared/notifications";
import Proxies from "../shared/proxies";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Tags from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import type { HttpJsonQueryForm } from "./schema";
import { deserialize, serialize } from "./schema";
import { useEffect } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const HttpJsonQuery = () => {
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

  const onSubmit = (data: HttpJsonQueryForm) => {
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
        onSubmit={form.handleSubmit((data) => onSubmit(data as HttpJsonQueryForm))}
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
            <TypographyH4>JSON Query Expression</TypographyH4>
            <div className="text-sm text-muted-foreground mb-4">
              Parse and extract specific data from the server's JSON response using JSON query or use "$" for the raw response, if not expecting JSON. The result is then compared to the expected value, as strings. See <a href="https://jsonata.org" target="_blank" rel="noopener noreferrer" className="underline">jsonata.org</a> for documentation and use <a href="https://jsonata.org/try" target="_blank" rel="noopener noreferrer" className="underline">playground</a> to experiment with queries.
            </div>
            
            <FormField
              control={form.control}
              name="json_query"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>JSON Query Expression</FormLabel>
                  <FormControl>
                    <Input 
                      placeholder="$"
                      {...field} 
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className="grid grid-cols-2 gap-4">
              <FormField
                control={form.control}
                name="json_condition"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Condition</FormLabel>
                    <Select onValueChange={field.onChange} defaultValue={field.value}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder="Select condition" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value="==">==</SelectItem>
                        <SelectItem value="!=">!=</SelectItem>
                        <SelectItem value=">">&gt;</SelectItem>
                        <SelectItem value="<">&lt;</SelectItem>
                        <SelectItem value=">=">&gt;=</SelectItem>
                        <SelectItem value="<=">&lt;=</SelectItem>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="expected_value"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Expected Value</FormLabel>
                    <FormControl>
                      <Input 
                        placeholder="Expected value"
                        {...field} 
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
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

export default HttpJsonQuery;