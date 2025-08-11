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
      toast.success("Password updated successfully");
      form.reset();
    },
    onError: commonMutationErrorHandler("Failed to update password"),
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
      <TypographyH4>Update password</TypographyH4>
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
                <FormLabel>Old Password</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="current-password"
                    placeholder="Enter your current password"
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
                <FormLabel>New Password</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="new-password"
                    placeholder="Enter a new password"
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
                <FormLabel>Confirm New Password</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete="new-password"
                    placeholder="Re-enter new password"
                    {...field}
                  />
                </FormControl>

                <FormMessage />
              </FormItem>
            )}
          />
          <Button type="submit">Update Password</Button>
        </form>
      </Form>
    </div>
  );
};

export default UpdatePassword;
