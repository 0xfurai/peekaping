import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AlertCircle } from "lucide-react";
import {
  getInvitesByTokenOptions,
  getMeOptions,
  postAuthRegisterMutation,
  postInvitesByTokenAcceptMutation,
} from "@/api/@tanstack/react-query.gen";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { PasswordInput } from "@/components/ui/password-input";
import { Loading } from "@/components/loading";
import { useEnterWorkspace } from "@/hooks/useEnterWorkspace";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { acceptInviteAndEnter } from "@/lib/accept-invite-and-enter";
import { inviteNextPath } from "@/lib/next-path";
import { workspaceErrorKey, workspaceErrorMessage } from "@/lib/workspace-errors";
import { useAuthStore } from "@/store/auth";

type Preview = {
  workspaceName: string;
  email: string;
  role: "member" | "admin" | "owner";
  status: "pending" | "accepted" | "revoked" | "expired";
  valid: boolean;
};

const Failure = ({ message }: { message: string }) => {
  const { t } = useLocalizedTranslation();
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{t("common.error")}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
};

// A signed-in user whose email is the invited one joins with one click
function AcceptInvite({ token, preview }: { token: string; preview: Preview }) {
  const { t } = useLocalizedTranslation();
  const enterWorkspace = useEnterWorkspace();
  const [failure, setFailure] = useState<string | null>(null);
  // The workspace a try joined but could not open. The invite is single-use, so a retry opens
  // this workspace instead of sending the invite again.
  const [acceptedId, setAcceptedId] = useState<string | null>(null);
  const [entering, setEntering] = useState(false);

  const accept = useMutation({
    ...postInvitesByTokenAcceptMutation(),
  });

  const submit = async () => {
    setFailure(null);
    setEntering(true);
    try {
      const result = await acceptInviteAndEnter({
        acceptedId,
        accept: async () => (await accept.mutateAsync({ path: { token } })).data.id,
        enter: enterWorkspace,
      });
      if (result.kind === "accept_failed") {
        setFailure(workspaceErrorMessage(result.error, t, t("members.errors.generic")));
      } else if (result.kind === "enter_failed") {
        console.error("Failed to enter the workspace the invite joined", result.error);
        setAcceptedId(result.workspaceId);
        setFailure(t("invite.enter_failed"));
      }
    } finally {
      setEntering(false);
    }
  };

  const busy = accept.isPending || entering;
  const joined = acceptedId !== null;

  return (
    <div className="grid gap-4">
      {failure && <Failure message={failure} />}
      <Button type="button" className="w-full" disabled={busy} onClick={() => void submit()}>
        {joined
          ? busy
            ? t("workspaces.entering")
            : t("workspaces.enter")
          : busy
            ? t("invite.accepting")
            : t("invite.accept")}
      </Button>
      {failure && !joined && (
        <Button asChild variant="outline" className="w-full">
          <Link to="/">{t("invite.open_app")}</Link>
        </Button>
      )}
      <p className="text-center text-sm text-muted-foreground">
        {t("invite.for_email", { email: preview.email })}
      </p>
    </div>
  );
}

// A signed-in user with another email: they have to sign out first
function OtherAccount({ currentEmail, preview }: { currentEmail: string; preview: Preview }) {
  const { t } = useLocalizedTranslation();
  const clearTokens = useAuthStore((state) => state.clearTokens);
  return (
    <div className="grid gap-4">
      <Alert>
        <AlertCircle className="h-4 w-4" />
        <AlertDescription>
          {t("invite.other_account", { current: currentEmail, invited: preview.email })}
        </AlertDescription>
      </Alert>
      <Button type="button" variant="outline" className="w-full" onClick={clearTokens}>
        {t("invite.sign_out")}
      </Button>
    </div>
  );
}

const createSignupSchema = (t: (key: string) => string) =>
  z.object({
    password: z.string().min(8, t("forms.validation.password_min_length")),
  });

type SignupFormValues = z.infer<ReturnType<typeof createSignupSchema>>;

// A visitor signs up as the invited email, and lands in the inviting workspace
function SignUpThroughInvite({ token, preview }: { token: string; preview: Preview }) {
  const { t } = useLocalizedTranslation();
  const navigate = useNavigate();
  const setTokens = useAuthStore((state) => state.setTokens);
  const setUser = useAuthStore((state) => state.setUser);
  const [failure, setFailure] = useState<string | null>(null);

  const form = useForm<SignupFormValues>({
    resolver: zodResolver(createSignupSchema(t)),
    defaultValues: { password: "" },
  });

  const register = useMutation({
    ...postAuthRegisterMutation(),
    onSuccess: (response) => {
      const { accessToken, refreshToken, user } = response.data ?? {};
      if (!accessToken || !refreshToken) {
        setFailure(t("messages.register_no_tokens"));
        return;
      }
      setTokens(accessToken, refreshToken);
      setUser(user ?? null);
      // The new account's only workspace is the inviting one, which is the default one
      navigate("/", { replace: true });
    },
    onError: (error) => setFailure(workspaceErrorMessage(error, t, t("messages.register_error"))),
  });

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => {
          setFailure(null);
          register.mutate({
            body: { email: preview.email, password: values.password, inviteToken: token },
          });
        })}
        className="grid gap-6"
      >
        <div className="grid gap-2">
          <label htmlFor="invite-email" className="text-sm font-medium leading-none">
            {t("forms.labels.email")}
          </label>
          <Input id="invite-email" type="email" readOnly value={preview.email} />
        </div>

        <FormField
          control={form.control}
          name="password"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("forms.labels.password")}</FormLabel>
              <FormControl>
                <PasswordInput {...field} placeholder="********" autoComplete="new-password" />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {failure && <Failure message={failure} />}

        <Button type="submit" className="w-full" disabled={register.isPending}>
          {register.isPending ? t("invite.creating") : t("invite.signup_submit")}
        </Button>

        <div className="text-center text-sm text-muted-foreground">
          {t("invite.have_account")}{" "}
          <Link to={inviteNextPath(token)} className="font-medium text-primary hover:underline">
            {t("invite.sign_in_instead")}
          </Link>
        </div>
      </form>
    </Form>
  );
}

// The page an invite link opens (/invite/:token). It needs no sign-in to look at the invite.
export function InviteLanding({ token }: { token: string }) {
  const signedIn = useAuthStore((state) => Boolean(state.accessToken));
  return <InviteLandingView token={token} signedIn={signedIn} />;
}

// The page for a visitor or a signed-in user. The sign-in state is a prop so that a static render
// can show both, since it reads a store's initial state.
export function InviteLandingView({ token, signedIn }: { token: string; signedIn: boolean }) {
  const { t } = useLocalizedTranslation();
  const previewQuery = useQuery(getInvitesByTokenOptions({ path: { token } }));
  const meQuery = useQuery({ ...getMeOptions(), enabled: signedIn });

  const preview = previewQuery.data?.data;
  const currentEmail = meQuery.data?.data?.user.email;

  const card = (title: string, description: string | null, body: React.ReactNode) => (
    <Card>
      <CardHeader className="text-center">
        <CardTitle className="text-xl">{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );

  if (previewQuery.isPending || (signedIn && meQuery.isPending)) return <Loading />;

  if (!preview) {
    // The server doesn't know the token (404), or failed
    const status = (previewQuery.error as { response?: { status?: number } } | null)?.response?.status;
    return card(
      t("invite.unavailable.title"),
      null,
      <p className="text-center text-sm">
        {status === 404 || status === undefined
          ? t(workspaceErrorKey(404, "Invite not found") ?? "invite.unavailable.not_found")
          : t("members.errors.generic")}
      </p>
    );
  }

  if (!preview.valid) {
    return card(
      t("invite.unavailable.title"),
      null,
      <p className="text-center text-sm">
        {t(`invite.unavailable.${preview.status === "pending" ? "unknown" : preview.status}`)}
      </p>
    );
  }

  const role = t(`members.roles.${preview.role}`);
  const matches = currentEmail?.toLowerCase() === preview.email.toLowerCase();

  if (signedIn) {
    return card(
      t("invite.title", { workspace: preview.workspaceName }),
      t("invite.description", { workspace: preview.workspaceName, role }),
      matches ? (
        <AcceptInvite token={token} preview={preview} />
      ) : (
        <OtherAccount currentEmail={currentEmail ?? ""} preview={preview} />
      )
    );
  }

  return card(
    t("invite.title", { workspace: preview.workspaceName }),
    t("invite.signup_description", { workspace: preview.workspaceName, role }),
    <SignUpThroughInvite token={token} preview={preview} />
  );
}
