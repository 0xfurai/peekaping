import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Loader2 } from "lucide-react";
import { useEffect } from "react";
import {
  sqlServerDefaultValues,
  serialize,
  type SQLServerForm as SQLServerFormType,
} from "./schema";

const SQLServerForm = () => {
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

  const onSubmit = (data: SQLServerFormType) => {
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
      form.reset(sqlServerDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) =>
          onSubmit(data as SQLServerFormType)
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
            <TypographyH4>Microsoft SQL Server Connection</TypographyH4>
            <FormField
              control={form.control}
              name="database_connection_string"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Connection String</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="Server=localhost,1433;Database=mydb;User Id=sa;Password=..."
                      {...field}
                    />
                  </FormControl>
                  <div className="text-sm text-muted-foreground space-y-3">
                    <div>
                      <p className="font-medium mb-2">Connection string format:</p>
                      <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                        Server=&lt;hostname&gt;,&lt;port&gt;;Database=&lt;database&gt;;User Id=&lt;username&gt;;Password=&lt;password&gt;;Encrypt=&lt;true/false&gt;;TrustServerCertificate=&lt;true/false&gt;;Connection Timeout=&lt;seconds&gt;
                      </code>
                    </div>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="database_query"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Query</FormLabel>
                  <FormControl>
                    <Textarea placeholder="SELECT 1" {...field} />
                  </FormControl>
                  <div className="text-sm text-muted-foreground">
                    <p>
                      Query to execute for health check. Leave empty to use default{" "}
                      <code className="text-xs bg-muted px-1 py-0.5 rounded">SELECT 1</code>.
                    </p>
                    <p className="mt-1">
                      <span className="font-medium">Allowed statements:</span> SELECT, SHOW, DESCRIBE, EXPLAIN, WITH, and VALUES.
                    </p>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
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

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? "Create Monitor" : "Update Monitor"}
        </Button>
      </form>
    </Form>
  );
};

export default SQLServerForm;
