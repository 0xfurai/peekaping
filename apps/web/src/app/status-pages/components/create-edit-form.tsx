import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import SearchableMonitorSelector from "@/components/searchable-monitor-selector";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const statusPageSchema = z.object({
  title: z.string().min(1, "Title is required"),
  slug: z
    .string()
    .min(1, "Slug is required")
    .regex(
      /^[a-z0-9-]+$/,
      "Slug must contain only lowercase letters, numbers, and hyphens"
    ),
  description: z.string().optional(),
  icon: z.string().optional(),
  footer_text: z.string().optional(),
  auto_refresh_interval: z.number().min(0).optional(),
  published: z.boolean(),
  monitors: z
    .array(
      z.object({
        label: z.string(),
        value: z.string(),
      })
    )
    .optional(),
});

export type StatusPageForm = z.infer<typeof statusPageSchema>;

const formDefaultValues: StatusPageForm = {
  title: "",
  slug: "",
  description: "",
  icon: "",
  footer_text: "",
  auto_refresh_interval: 300,
  published: true,
  monitors: [],
};

const CreateEditForm = ({
  onSubmit,
  initialValues,
  isPending,
  mode = "create",
}: {
  onSubmit: (data: StatusPageForm) => void;
  initialValues?: StatusPageForm;
  isPending?: boolean;
  mode?: "create" | "edit";
}) => {
  const { t } = useLocalizedTranslation();
  const form = useForm<StatusPageForm>({
    defaultValues: initialValues || formDefaultValues,
    resolver: zodResolver(statusPageSchema),
  });

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardHeader>
            <CardTitle>{t("status_pages.form.basic_information_title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
              <FormField
                control={form.control}
                name="title"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("status_pages.form.title_label")}</FormLabel>
                    <FormControl>
                      <Input placeholder={t("status_pages.form.title_placeholder")} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="slug"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("status_pages.form.slug_label")}</FormLabel>
                    <FormControl>
                      <Input placeholder="status-page-slug" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <FormField
              control={form.control}
              name="description"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("status_pages.form.description_label")}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t("status_pages.form.description_placeholder")}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="icon"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("status_pages.form.icon_url_label")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="https://example.com/icon.png"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className="space-y-4">
              <h2 className="text-lg font-semibold">{t("status_pages.form.affected_monitors_title")}</h2>
              <div className="space-y-2">
                <p className="text-sm text-muted-foreground">
                  {t("status_pages.form.affected_monitors_description")}
                </p>

                <SearchableMonitorSelector
                  value={form.watch("monitors") || []}
                  onSelect={(value) => {
                    form.setValue("monitors", value);
                  }}
                />
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("status_pages.form.customization_title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <FormField
              control={form.control}
              name="footer_text"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("forms.labels.footer_text")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("status_pages.form.footer_text_placeholder")} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="auto_refresh_interval"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("status_pages.form.auto_refresh_interval_label")}</FormLabel>
                  <FormControl>
                    <Input
                      type="number"
                      min="0"
                      placeholder={t("status_pages.form.auto_refresh_interval_placeholder")}
                      {...field}
                      onChange={(e) => field.onChange(e.target.valueAsNumber)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("common.settings")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <FormField
              control={form.control}
              name="published"
              render={({ field }) => (
                <FormItem>
                  <div className="flex items-center justify-between">
                    <div className="space-y-0.5">
                      <FormLabel>{t("status_pages.published")}</FormLabel>
                      <p className="text-sm text-muted-foreground">
                        {t("status_pages.form.published_description")}
                      </p>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <div className="flex justify-end space-x-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => window.history.back()}
          >
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={isPending}>
            {isPending
              ? t("common.saving")
              : mode === "create"
              ? t("status_pages.form.create_button")
              : t("status_pages.form.update_button")}
          </Button>
        </div>
      </form>
    </Form>
  );
};

export default CreateEditForm;
