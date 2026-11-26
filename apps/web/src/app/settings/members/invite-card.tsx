import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import {
  getWorkspaceInvitesQueryKey,
  postWorkspaceInvitesMutation,
} from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { invitableRoles, type MemberRole } from "@/lib/member-roles";
import { workspaceErrorMessage } from "@/lib/workspace-errors";
import { RoleSelect } from "./role-select";

const createSchema = (t: (key: string) => string) =>
  z.object({
    email: z.string().email(t("forms.validation.email_invalid")),
    role: z.enum(["member", "admin", "owner"]),
  });

type InviteFormValues = z.infer<ReturnType<typeof createSchema>>;

// The invite form of an admin or owner. The server answers the invite's link once, so it is
// kept in this component's state only, until it is dismissed.
export function InviteCard({ actor }: { actor: MemberRole }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const [link, setLink] = useState<string | null>(null);
  const roles = invitableRoles(actor);

  const form = useForm<InviteFormValues>({
    resolver: zodResolver(createSchema(t)),
    defaultValues: { email: "", role: "member" },
  });

  const mutation = useMutation({
    ...postWorkspaceInvitesMutation(),
    onSuccess: (response) => {
      setLink(response.data.link);
      form.reset();
      queryClient.invalidateQueries({ queryKey: getWorkspaceInvitesQueryKey() });
    },
    onError: (error) =>
      toast.error(workspaceErrorMessage(error, t, t("members.errors.generic"))),
  });

  const copy = async () => {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      toast.success(t("members.invite.copied"));
    } catch (error) {
      console.error("Failed to copy the invite link", error);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("members.invite.title")}</CardTitle>
        <CardDescription>{t("members.invite.description")}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((values) => {
              setLink(null);
              mutation.mutate({ body: values });
            })}
            className="flex flex-wrap items-start gap-2"
          >
            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem className="min-w-56 flex-1">
                  <FormLabel>{t("members.invite.email_label")}</FormLabel>
                  <FormControl>
                    <Input type="email" placeholder="name@example.com" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="role"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("members.invite.role_label")}</FormLabel>
                  <FormControl>
                    <RoleSelect roles={roles} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button type="submit" className="mt-[1.375rem]" disabled={mutation.isPending}>
              {mutation.isPending ? t("members.invite.creating") : t("members.invite.submit")}
            </Button>
          </form>
        </Form>

        {link && (
          <Alert>
            <AlertTitle>{t("members.invite.link_title")}</AlertTitle>
            <AlertDescription className="grid gap-2">
              <div className="flex gap-2">
                <Input readOnly value={link} dir="ltr" onFocus={(e) => e.currentTarget.select()} />
                <Button type="button" variant="outline" onClick={copy}>
                  <Copy />
                  {t("members.invite.copy")}
                </Button>
              </div>
              <p>{t("members.invite.link_note")}</p>
              <div>
                <Button type="button" variant="ghost" size="sm" onClick={() => setLink(null)}>
                  {t("members.invite.done")}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
