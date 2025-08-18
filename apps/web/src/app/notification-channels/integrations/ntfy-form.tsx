import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import * as React from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("ntfy"),
  server_url: z.string().url({ message: "Valid NTFY server URL is required" }),
  topic: z.string().min(1, { message: "Topic is required" }),
  authentication_type: z.enum(["none", "basic", "token"]),
  username: z.string().optional(),
  password: z.string().optional(),
  token: z.string().optional(),
  priority: z.coerce.number().min(1).max(5),
  tags: z.string().optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export type NtfyFormValues = z.infer<typeof schema>;

export const defaultValues: NtfyFormValues = {
  type: "ntfy",
  server_url: "https://ntfy.sh",
  topic: "peekaping",
  authentication_type: "none",
  username: "",
  password: "",
  token: "",
  priority: 3,
  tags: "peekaping,monitoring",
  title: "Peekaping Alert - {{ name }}",
  custom_message: "{{ msg }}",
};

export const displayName = "NTFY";

export default function NtfyForm() {
  const form = useFormContext();
  const authType = form.watch("authentication_type");
  const { t } = useLocalizedTranslation();

  // Handle conditional validation
  React.useEffect(() => {
    if (authType === "basic") {
      form.clearErrors(["username", "password"]);
      if (!form.getValues("username")) {
        form.setError("username", {
          message: t("notifications.form.ntfy.username_required"),
        });
      }
      if (!form.getValues("password")) {
        form.setError("password", {
          message: t("notifications.form.ntfy.password_required"),
        });
      }
    } else if (authType === "token") {
      form.clearErrors(["token"]);
      if (!form.getValues("token")) {
        form.setError("token", {
          message: t("notifications.form.ntfy.token_required"),
        });
      }
    }
  }, [authType, form, t]);

  return (
    <>
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.server_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="https://ntfy.sh"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.ntfy.server_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="topic"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.topic_label")}</FormLabel>
            <FormControl>
              <Input placeholder="peekaping" required {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.ntfy.topic_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication_type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.authentication_type_label")}</FormLabel>
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
                  <SelectValue placeholder={t("notifications.form.ntfy.authentication_type_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="none">{t("notifications.form.ntfy.authentication_type_none")}</SelectItem>
                <SelectItem value="basic">{t("notifications.form.ntfy.authentication_type_basic")}</SelectItem>
                <SelectItem value="token">{t("notifications.form.ntfy.authentication_type_token")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {authType === "none" && (
                <>
                  {t("notifications.form.ntfy.authentication_type_none_description")}
                </>
              )}
              {authType === "basic" && (
                <>
                  {t("notifications.form.ntfy.authentication_type_basic_description")}
                </>
              )}
              {authType === "token" && (
                <>
                  {t("notifications.form.ntfy.authentication_type_token_description")}
                </>
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {authType === "basic" && (
        <>
          <FormField
            control={form.control}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("forms.labels.username")}</FormLabel>
                <FormControl>
                  <Input placeholder="your-username" required {...field} />
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
                  <PasswordInput
                    placeholder="your-password"
                    required
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </>
      )}

      {authType === "token" && (
        <FormField
          control={form.control}
          name="token"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.ntfy.token_label")}</FormLabel>
              <FormControl>
                <PasswordInput
                  placeholder="tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2"
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t("notifications.form.ntfy.token_description")}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={form.control}
        name="priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.priority_label")}</FormLabel>
            <Select
              onValueChange={(val) => {
                if (!val) {
                  return;
                }
                field.onChange(parseInt(val));
              }}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.ntfy.priority_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="1">1 - {t("notifications.form.ntfy.priority_1")}</SelectItem>
                <SelectItem value="2">2 - {t("notifications.form.ntfy.priority_2")}</SelectItem>
                <SelectItem value="3">3 - {t("notifications.form.ntfy.priority_3")}</SelectItem>
                <SelectItem value="4">4 - {t("notifications.form.ntfy.priority_4")}</SelectItem>
                <SelectItem value="5">5 - {t("notifications.form.ntfy.priority_5")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.ntfy.priority_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="tags"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.tags_label")}</FormLabel>
            <FormControl>
              <Input placeholder="peekaping,monitoring,alert" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.ntfy.tags_description")}: {"{{ name }}"}, {"{{ status }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.title_label")}</FormLabel>
            <FormControl>
              <Input placeholder="Peekaping Alert - {{ name }}" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.ntfy.title_description")}:{" "}
              {"{{ name }}"}, {"{{ status }}"}, {"{{ msg }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.ntfy.custom_message_label")}</FormLabel>
            <FormControl>
              <Textarea
                placeholder="{{ msg }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.ntfy.custom_message_description")}: {"{{ msg }}"},{" "}
              {"{{ name }}"}, {"{{ status }}"}, {"{{ monitor.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
