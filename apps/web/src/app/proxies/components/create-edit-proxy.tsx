import { zodResolver } from "@hookform/resolvers/zod";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { z } from "zod";
import { useForm, useWatch } from "react-hook-form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const formSchema = z.object({
  protocol: z.enum(["http", "https", "socks", "socks5", "socks5h", "socks4"]),
  host: z.string().min(1, { message: "Host is required" }),
  port: z
    .number()
    .min(1, { message: "Port must be at least 1" })
    .max(65535, { message: "Port must be between 1 and 65535" }),
  auth: z.boolean(),
  username: z.string().optional(),
  password: z.string().optional(),
});

export type ProxyForm = z.infer<typeof formSchema>;

const proxyProtocols = [
  { type: "http", description: "HTTP" },
  { type: "https", description: "HTTPS" },
  { type: "socks", description: "SOCKS" },
  { type: "socks5", description: "SOCKS v5" },
  { type: "socks5h", description: "SOCKS v5 (+DNS)" },
  { type: "socks4", description: "SOCKS v4" },
];

type CreateEditProxyProps = {
  initialValues?: ProxyForm;
  mode?: "create" | "edit";
  isLoading?: boolean;
  onSubmit: (data: ProxyForm) => void;
};

const defaultValues: ProxyForm = {
  protocol: "https",
  host: "",
  port: 80,
  auth: false,
  username: "",
  password: "",
};

export default function CreateEditProxy({
  onSubmit,
  initialValues,
  mode = "create",
  isLoading = false,
}: CreateEditProxyProps) {
  const { t } = useLocalizedTranslation();
  const form = useForm<ProxyForm>({
    defaultValues: initialValues || defaultValues,
    resolver: zodResolver(formSchema),
  });

  const { isSubmitting } = form.formState;

  const auth = useWatch({
    control: form.control,
    name: "auth",
  });

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6 max-w-[600px]"
      >
        <FormField
          control={form.control}
          name="protocol"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("proxies.form.protocol_label")}</FormLabel>
              <Select onValueChange={field.onChange} value={field.value}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder={t("proxies.form.protocol_placeholder")} />
                  </SelectTrigger>
                </FormControl>

                <SelectContent>
                  {proxyProtocols.map((item) => (
                    <SelectItem key={item.type} value={item.type}>
                      {item.description}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <FormMessage />
            </FormItem>
          )}
        />

        <FormItem>
          <FormLabel>{t("proxies.form.server_label")}</FormLabel>
          <div className="flex space-x-4">
            <FormField
              control={form.control}
              name="host"
              render={({ field }) => (
                <>
                  <Input placeholder={t("proxies.form.server_placeholder")} {...field} />
                  <FormMessage />
                </>
              )}
            />

            <FormField
              control={form.control}
              name="port"
              render={({ field }) => (
                <>
                  <Input
                    placeholder={t("proxies.form.port_placeholder")}
                    {...field}
                    type="number"
                    value={field.value}
                    onChange={(e) => field.onChange(Number(e.target.value))}
                  />
                  <FormMessage />
                </>
              )}
            />
          </div>
        </FormItem>

        <FormField
          control={form.control}
          name="auth"
          render={({ field }) => (
            <FormItem className="flex flex-row items-center justify-between rounded-lg border p-3 shadow-sm">
              <div className="space-y-0.5">
                <FormLabel>{t("proxies.form.auth_label")}</FormLabel>
              </div>

              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                  aria-readonly
                />
              </FormControl>
            </FormItem>
          )}
        />

        {auth && (
          <>
            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("proxies.form.user")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("proxies.form.user")} {...field} />
                  </FormControl>
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
                    <PasswordInput placeholder={t("forms.labels.password")} {...field} />
                  </FormControl>

                  <FormMessage />
                </FormItem>
              )}
            />
          </>
        )}

        <Button type="submit" disabled={isSubmitting || isLoading}>
          {(isSubmitting || isLoading) && (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          )}
          {isSubmitting || isLoading ? t("common.saving") : mode === "create" ? t("common.save") : t("common.update")}
        </Button>
      </form>
    </Form>
  );
}
