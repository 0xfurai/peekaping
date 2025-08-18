import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { PasswordInput } from "@/components/ui/password-input";
import { Button } from "@/components/ui/button";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { putAuthPasswordMutation } from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useMutation } from "@tanstack/react-query";
import { TypographyH4 } from "@/components/ui/typography";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const passwordSchema = z
  .object({
    currentPassword: z.string().min(1, { message: "Old password is required" }),
    newPassword: z
      .string()
      .min(8, { message: "New password must be at least 8 characters" }),
    confirmPassword: z
      .string()
      .min(1, { message: "Please confirm new password" }),
  })
  .refine((data) => data.newPassword === data.confirmPassword, {
    message: "Passwords do not match",
    path: ["confirmPassword"],
  });

type PasswordFormType = z.infer<typeof passwordSchema>;

const UpdatePassword = () => {
  const { t } = useLocalizedTranslation();
  const form = useForm<PasswordFormType>({
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
    resolver: zodResolver(passwordSchema),
  });

  const updatePasswordMutation = useMutation({
    ...putAuthPasswordMutation(),
    onSuccess: () => {
      toast.success(t("security.update_password.messages.password_updated_successfully"));
      form.reset();
    },
    onError: commonMutationErrorHandler(t("security.update_password.messages.failed_to_update_password")),
  });

  const onSubmit = (data: PasswordFormType) => {
    updatePasswordMutation.mutate({
      body: {
        currentPassword: data.currentPassword,
        newPassword: data.newPassword,
      },
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <TypographyH4>{t("security.update_password.title")}</TypographyH4>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="space-y-6 max-w-[600px]"
        >
          <FormField
            control={form.control}
            name="currentPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("security.update_password.form.old_password_label")}</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="current-password"
                    placeholder={t("security.update_password.form.old_password_placeholder")}
                    {...field}
                  />
                </FormControl>

                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="newPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("security.update_password.form.new_password_label")}</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="new-password"
                    placeholder={t("security.update_password.form.new_password_placeholder")}
                    {...field}
                  />
                </FormControl>

                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="confirmPassword"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("security.update_password.form.confirm_new_password_label")}</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="new-password"
                    placeholder={t("security.update_password.form.confirm_new_password_placeholder")}
                    {...field}
                  />
                </FormControl>

                <FormMessage />
              </FormItem>
            )}
          />
          <Button type="submit">{t("security.update_password.form.update_password_button")}</Button>
        </form>
      </Form>
    </div>
  );
};

export default UpdatePassword;
