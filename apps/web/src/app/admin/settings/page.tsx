import { useEffect, useState, type ComponentProps, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import {
  getAdminSettingsOptions,
  getAdminSettingsQueryKey,
  postAdminSettingsTestEmailMutation,
  putAdminSettingsMutation,
} from "@/api/@tanstack/react-query.gen";
import type { AdminInstallSettingsResponse } from "@/api/types.gen";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { PasswordInput } from "@/components/ui/password-input";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  installSettingsChange,
  installSettingsErrorMessage,
  installSettingsFormValues,
  type InstallSettingsFormValues,
  type SettingSource,
} from "@/lib/install-settings";
import { cn } from "@/lib/utils";
import Layout from "@/layout";
import { ErrorReportingSetting, TelemetrySetting } from "./telemetry-settings";

const createSchema = (t: (key: string) => string) =>
  z.object({
    signupMode: z.string(),
    publicUrl: z.string(),
    host: z.string(),
    port: z.coerce
      .number()
      .int(t("admin.settings.validation.port"))
      .min(0, t("admin.settings.validation.port"))
      .max(65535, t("admin.settings.validation.port")),
    user: z.string(),
    password: z.string(),
    from: z.string(),
    tls: z.string(),
  });

type Setting = { source: SettingSource; locked: boolean; envVar: string };

// A setting's label with where its value comes from, the field, and, for a field the server's
// environment sets, the note that locks it
function SettingField({
  id,
  label,
  setting,
  error,
  children,
}: {
  id: string;
  label: string;
  setting: Setting;
  error?: string;
  children: ReactNode;
}) {
  const { t } = useLocalizedTranslation();
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Label htmlFor={id}>{label}</Label>
        <span className="text-xs text-muted-foreground">
          {t(`admin.settings.source.${setting.source}`)}
        </span>
      </div>
      {children}
      {setting.locked && (
        <p className="text-xs text-muted-foreground">
          {t("admin.settings.locked")} (<code dir="ltr">{setting.envVar}</code>)
        </p>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}

// A native select styled like an Input, as the members page's role select is
function NativeSelect({ className, ...props }: ComponentProps<"select">) {
  return (
    <select
      data-slot="select"
      className={cn(
        "border-input dark:bg-input/30 h-9 rounded-md border bg-transparent px-2 text-sm shadow-xs outline-none disabled:cursor-not-allowed disabled:opacity-50",
        "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
        className
      )}
      {...props}
    />
  );
}

const option = "bg-popover text-popover-foreground";

// The signup mode, the sender and the public URL, saved together: a save sends only what changed,
// so that open signup and the sender it needs can be set in one go.
function InstallSettingsForm({ settings }: { settings: AdminInstallSettingsResponse }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const [removePassword, setRemovePassword] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const form = useForm({
    resolver: zodResolver(createSchema(t)),
    values: installSettingsFormValues(settings),
    // A refetch while the admin types keeps what they typed
    resetOptions: { keepDirtyValues: true },
  });
  const watched = form.watch();
  const pendingChange = installSettingsChange(
    settings,
    { ...watched, port: Number(watched.port) || 0 },
    { removePassword }
  );
  const hasChanges = Object.keys(pendingChange).length > 0;
  // A field shows the form's value, so that what loaded is in the markup before any effect runs
  const bind = (name: keyof InstallSettingsFormValues) => ({
    ...form.register(name),
    value: watched[name] ?? "",
  });
  // A select can't be read-only, and the form reads no value from a disabled one: a locked select
  // shows its value outside the form, which keeps the loaded one
  const bindSelect = (name: "signupMode" | "tls", setting: { value: string; locked: boolean }) =>
    setting.locked ? { disabled: true, value: setting.value, onChange: () => {} } : bind(name);

  const mutation = useMutation({
    ...putAdminSettingsMutation(),
    onSuccess: (response) => {
      queryClient.setQueryData(getAdminSettingsQueryKey(), response);
      form.reset(installSettingsFormValues(response.data));
      setRemovePassword(false);
      setSaveError(null);
      toast.success(t("admin.settings.saved"));
    },
    onError: (error) => setSaveError(installSettingsErrorMessage(error, t, t("admin.settings.save_failed"))),
  });

  const submit = (values: InstallSettingsFormValues) => {
    const body = installSettingsChange(settings, values, { removePassword });
    if (Object.keys(body).length === 0) return;
    setSaveError(null);
    mutation.mutate({ body });
  };

  const smtp = settings.accountSmtp;
  const errors = form.formState.errors;
  const passwordTyped = watched.password !== "";

  return (
    <form onSubmit={form.handleSubmit(submit)} className="flex flex-col gap-6" noValidate>
      <Card>
        <CardHeader>
          <CardTitle>{t("admin.settings.signup.title")}</CardTitle>
          <CardDescription>{t("admin.settings.signup.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <SettingField id="install-signup-mode" label={t("admin.settings.signup.label")} setting={settings.signupMode}>
            <NativeSelect id="install-signup-mode" {...bindSelect("signupMode", settings.signupMode)}>
              <option value="invite" className={option}>{t("admin.settings.signup.invite")}</option>
              <option value="open" className={option}>{t("admin.settings.signup.open")}</option>
            </NativeSelect>
            <p className="text-sm text-muted-foreground">
              {watched.signupMode === "open"
                ? t("admin.settings.signup.open_help")
                : t("admin.settings.signup.invite_help")}
            </p>
          </SettingField>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("admin.settings.sender.title")}</CardTitle>
          <CardDescription>{t("admin.settings.sender.description")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="text-sm">
            {settings.accountEmails ? t("admin.settings.sender.on") : t("admin.settings.sender.off")}
          </p>
          <SettingField id="install-smtp-host" label={t("admin.settings.sender.host")} setting={smtp.host}>
            <Input id="install-smtp-host" dir="ltr" placeholder="smtp.example.com" readOnly={smtp.host.locked} {...bind("host")} />
          </SettingField>
          <SettingField id="install-smtp-port" label={t("admin.settings.sender.port")} setting={smtp.port} error={errors.port?.message}>
            <Input id="install-smtp-port" type="number" min={0} max={65535} readOnly={smtp.port.locked} {...bind("port")} />
          </SettingField>
          <SettingField id="install-smtp-tls" label={t("admin.settings.sender.tls")} setting={smtp.tls}>
            <NativeSelect id="install-smtp-tls" {...bindSelect("tls", smtp.tls)}>
              <option value="starttls" className={option}>{t("admin.settings.sender.tls_starttls")}</option>
              <option value="tls" className={option}>{t("admin.settings.sender.tls_tls")}</option>
              <option value="none" className={option}>{t("admin.settings.sender.tls_none")}</option>
            </NativeSelect>
          </SettingField>
          <SettingField id="install-smtp-user" label={t("admin.settings.sender.user")} setting={smtp.user}>
            <Input id="install-smtp-user" dir="ltr" autoComplete="off" readOnly={smtp.user.locked} {...bind("user")} />
          </SettingField>
          <SettingField id="install-smtp-password" label={t("admin.settings.sender.password")} setting={smtp.password}>
            {/* Write-only: the field never holds the saved password, and typing replaces it */}
            <PasswordInput
              id="install-smtp-password"
              autoComplete="new-password"
              readOnly={smtp.password.locked}
              placeholder={
                removePassword && !passwordTyped
                  ? t("admin.settings.sender.password_removed")
                  : smtp.password.set
                    ? t("admin.settings.sender.password_set")
                    : t("admin.settings.sender.password_not_set")
              }
              {...bind("password")}
            />
            {smtp.password.set && !smtp.password.locked && !passwordTyped && (
              <div className="flex flex-wrap items-center gap-2">
                {removePassword ? (
                  <>
                    <span className="text-sm text-muted-foreground">
                      {t("admin.settings.sender.password_removed")}
                    </span>
                    <Button type="button" variant="ghost" size="sm" onClick={() => setRemovePassword(false)}>
                      {t("admin.settings.sender.keep_password")}
                    </Button>
                  </>
                ) : (
                  <Button type="button" variant="outline" size="sm" onClick={() => setRemovePassword(true)}>
                    {t("admin.settings.sender.remove_password")}
                  </Button>
                )}
              </div>
            )}
          </SettingField>
          <SettingField id="install-smtp-from" label={t("admin.settings.sender.from")} setting={smtp.from}>
            <Input id="install-smtp-from" dir="ltr" placeholder="Peekaping <no-reply@example.com>" readOnly={smtp.from.locked} {...bind("from")} />
          </SettingField>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("admin.settings.public_url.title")}</CardTitle>
          <CardDescription>{t("admin.settings.public_url.description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <SettingField id="install-public-url" label={t("admin.settings.public_url.label")} setting={settings.publicUrl}>
            <Input id="install-public-url" dir="ltr" type="url" placeholder="https://peekaping.example.com" readOnly={settings.publicUrl.locked} {...bind("publicUrl")} />
          </SettingField>
        </CardContent>
      </Card>

      {saveError && (
        <Alert variant="destructive" role="alert">
          <AlertDescription>{saveError}</AlertDescription>
        </Alert>
      )}
      <div>
        <Button type="submit" disabled={!hasChanges || mutation.isPending}>
          {mutation.isPending ? t("common.saving") : t("common.save")}
        </Button>
      </div>

      <TestEmailCard unsaved={pendingChange.accountSmtp !== undefined} />
    </form>
  );
}

// Sends a test email through the saved sender, to the admin's own address unless they type another.
// It sits in the settings form but sends nothing of it: Enter in its field sends the test email.
function TestEmailCard({ unsaved }: { unsaved: boolean }) {
  const { t } = useLocalizedTranslation();
  const ownEmail = useMe().data?.data?.user.email ?? "";
  const [to, setTo] = useState<string | null>(null);
  const address = to ?? ownEmail;
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);

  const mutation = useMutation({
    ...postAdminSettingsTestEmailMutation(),
    onSuccess: (_, variables) =>
      setResult({ ok: true, text: t("admin.settings.test_email.sent", { to: variables.body.to }) }),
    onError: (error) =>
      setResult({ ok: false, text: installSettingsErrorMessage(error, t, t("admin.settings.test_email.failed")) }),
  });

  const send = () => {
    if (!address.trim() || mutation.isPending) return;
    setResult(null);
    mutation.mutate({ body: { to: address.trim() } });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("admin.settings.test_email.title")}</CardTitle>
        <CardDescription>{t("admin.settings.test_email.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor="install-test-email-to">{t("admin.settings.test_email.to")}</Label>
          <div className="flex flex-wrap gap-2">
            <Input
              id="install-test-email-to"
              type="email"
              dir="ltr"
              className="min-w-56 flex-1"
              value={address}
              onChange={(event) => setTo(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  send();
                }
              }}
            />
            <Button type="button" variant="outline" disabled={!address.trim() || mutation.isPending} onClick={send}>
              {mutation.isPending ? t("admin.settings.test_email.sending") : t("admin.settings.test_email.send")}
            </Button>
          </div>
        </div>
        {unsaved && (
          <p className="text-sm text-muted-foreground">{t("admin.settings.test_email.unsaved")}</p>
        )}
        {result && (
          <Alert variant={result.ok ? "default" : "destructive"} role={result.ok ? "status" : "alert"}>
            <AlertDescription>{result.text}</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}

// The install's settings (/admin/settings): the signup mode, the sender of account emails, the
// public URL, and the switches of the usage ping and error reports (docs/adr/0005, section 5)
const InstallSettingsPage = () => {
  const { t } = useLocalizedTranslation();
  const { data, isLoading, isError } = useQuery(getAdminSettingsOptions());
  const settings = data?.data;
  const loaded = settings !== undefined;

  // The upgrade notice links to a card, such as /admin/settings#error-reports
  const { hash } = useLocation();
  useEffect(() => {
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView();
  }, [hash, loaded]);

  return (
    <Layout pageName={t("admin.settings.page_name")}>
      <div className="max-w-xl flex flex-col gap-6">
        <p className="text-sm text-muted-foreground">{t("admin.settings.description")}</p>
        {isError ? (
          <p className="text-sm text-destructive">{t("admin.settings.load_failed")}</p>
        ) : isLoading || !settings ? (
          <div className="h-10 flex items-center">{t("common.loading")}</div>
        ) : (
          <InstallSettingsForm settings={settings} />
        )}
        <TelemetrySetting />
        <ErrorReportingSetting />
      </div>
    </Layout>
  );
};

export default InstallSettingsPage;
