import { useEffect, useState, type ComponentProps, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { useIsMutating, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
import { ABSOLUTE_MIN_CHECK_INTERVAL } from "@/hooks/useMinCheckInterval";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  clearsSavedPassword,
  DEFAULT_LIMITS,
  defaultLimitBounds,
  defaultLimitsSchema,
  installSettingsChange,
  installSettingsErrorMessage,
  installSettingsFormValues,
  type DefaultLimit,
  type InstallSettingsFormValues,
  type SettingSource,
  SAVE_INSTALL_SETTINGS,
} from "@/lib/install-settings";
import { cn } from "@/lib/utils";
import Layout from "@/layout";
import { ErrorReportingSetting, TelemetrySetting } from "./telemetry-settings";

const createSchema = (
  t: (key: string, vars?: Record<string, unknown>) => string,
  minCheckInterval: number,
  settings: AdminInstallSettingsResponse
) =>
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
    workspaceLimits: defaultLimitsSchema(t, minCheckInterval, settings.workspaceLimits),
    workspaceLimitsName: z.string(),
  });

type Setting = { source: SettingSource; locked: boolean; envVar: string };

// A field is described by where its value comes from, and by the note that locks it
const describedBy = (id: string, setting: Setting) => ({
  "aria-describedby": setting.locked ? `${id}-source ${id}-locked` : `${id}-source`,
});

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
        <span id={`${id}-source`} className="text-xs text-muted-foreground">
          {t(`admin.settings.source.${setting.source}`)}
        </span>
      </div>
      {children}
      {setting.locked && (
        <p id={`${id}-locked`} className="text-xs text-muted-foreground">
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

// The signup mode, the sender, the public URL and the default limits with their name, saved
// together: a save sends only what changed, so that open signup and the sender it needs can be set
// in one go.
function InstallSettingsForm({ settings }: { settings: AdminInstallSettingsResponse }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const [removePassword, setRemovePassword] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  // The server holds the shortest check interval to the install's MIN_CHECK_INTERVAL, which /me
  // states; 20 while it loads
  const minCheckInterval = Math.max(
    useMe().data?.data?.minCheckInterval ?? ABSOLUTE_MIN_CHECK_INTERVAL,
    ABSOLUTE_MIN_CHECK_INTERVAL
  );

  const form = useForm({
    resolver: zodResolver(createSchema(t, minCheckInterval, settings)),
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
  const bind = (name: Exclude<keyof InstallSettingsFormValues, "workspaceLimits">) => ({
    ...form.register(name),
    value: watched[name] ?? "",
  });
  const bindLimit = (name: DefaultLimit) => ({
    ...form.register(`workspaceLimits.${name}`),
    value: watched.workspaceLimits?.[name] ?? "",
  });
  // A select can't be read-only, and the form reads no value from a disabled one: a locked select
  // shows its value outside the form, which keeps the loaded one
  const bindSelect = (name: "signupMode" | "tls", setting: { value: string; locked: boolean }) =>
    setting.locked ? { disabled: true, value: setting.value, onChange: () => {} } : bind(name);

  const mutation = useMutation({
    ...putAdminSettingsMutation(),
    mutationKey: [...SAVE_INSTALL_SETTINGS],
    onSuccess: (response) => {
      queryClient.setQueryData(getAdminSettingsQueryKey(), response);
      form.reset(installSettingsFormValues(response.data));
      setRemovePassword(false);
      setSaveError(null);
      toast.success(t("admin.settings.saved"));
    },
    onError: (error) => setSaveError(installSettingsErrorMessage(error, t, t("admin.settings.save_failed"))),
  });
  // While a save is pending the fields are disabled: its answer resets the form, which would drop
  // anything typed in the meantime
  const saving = useIsMutating({ mutationKey: [...SAVE_INSTALL_SETTINGS] }) > 0;

  const submit = (values: InstallSettingsFormValues) => {
    const body = installSettingsChange(settings, values, { removePassword });
    if (saving || Object.keys(body).length === 0) return;
    setSaveError(null);
    mutation.mutate({ body });
  };

  const smtp = settings.accountSmtp;
  const errors = form.formState.errors;
  const passwordTyped = watched.password !== "";
  // A change of the server or user without a password clears the saved one on the server
  const passwordGoes = clearsSavedPassword(settings, pendingChange);

  return (
    <form onSubmit={form.handleSubmit(submit)} className="flex flex-col gap-6" noValidate>
      <fieldset disabled={saving} className="flex min-w-0 flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("admin.settings.signup.title")}</CardTitle>
            <CardDescription>{t("admin.settings.signup.description")}</CardDescription>
          </CardHeader>
          <CardContent>
            <SettingField id="install-signup-mode" label={t("admin.settings.signup.label")} setting={settings.signupMode}>
              <NativeSelect id="install-signup-mode" {...describedBy("install-signup-mode", settings.signupMode)} {...bindSelect("signupMode", settings.signupMode)}>
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
              <Input id="install-smtp-host" {...describedBy("install-smtp-host", smtp.host)} dir="ltr" placeholder="smtp.example.com" readOnly={smtp.host.locked} {...bind("host")} />
            </SettingField>
            <SettingField id="install-smtp-port" label={t("admin.settings.sender.port")} setting={smtp.port} error={errors.port?.message}>
              <Input id="install-smtp-port" {...describedBy("install-smtp-port", smtp.port)} type="number" min={0} max={65535} readOnly={smtp.port.locked} {...bind("port")} />
            </SettingField>
            <SettingField id="install-smtp-tls" label={t("admin.settings.sender.tls")} setting={smtp.tls}>
              <NativeSelect id="install-smtp-tls" {...describedBy("install-smtp-tls", smtp.tls)} {...bindSelect("tls", smtp.tls)}>
                <option value="starttls" className={option}>{t("admin.settings.sender.tls_starttls")}</option>
                <option value="tls" className={option}>{t("admin.settings.sender.tls_tls")}</option>
                <option value="none" className={option}>{t("admin.settings.sender.tls_none")}</option>
              </NativeSelect>
            </SettingField>
            <SettingField id="install-smtp-user" label={t("admin.settings.sender.user")} setting={smtp.user}>
              <Input id="install-smtp-user" {...describedBy("install-smtp-user", smtp.user)} dir="ltr" autoComplete="off" readOnly={smtp.user.locked} {...bind("user")} />
            </SettingField>
            <SettingField id="install-smtp-password" label={t("admin.settings.sender.password")} setting={smtp.password}>
              {/* Write-only: the field never holds the saved password, and typing replaces it */}
              <PasswordInput
                id="install-smtp-password"
                {...describedBy("install-smtp-password", smtp.password)}
                autoComplete="new-password"
                readOnly={smtp.password.locked}
                placeholder={
                  (removePassword || passwordGoes) && !passwordTyped
                    ? t("admin.settings.sender.password_removed")
                    : smtp.password.set
                      ? t("admin.settings.sender.password_set")
                      : t("admin.settings.sender.password_not_set")
                }
                {...bind("password")}
              />
              {passwordGoes ? (
                <p className="text-sm text-muted-foreground">{t("admin.settings.sender.password_reenter")}</p>
              ) : smtp.password.set && !smtp.password.locked && !passwordTyped && (
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
              <Input id="install-smtp-from" {...describedBy("install-smtp-from", smtp.from)} dir="ltr" placeholder="Peekaping <no-reply@example.com>" readOnly={smtp.from.locked} {...bind("from")} />
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
              <Input id="install-public-url" {...describedBy("install-public-url", settings.publicUrl)} dir="ltr" type="url" placeholder="https://peekaping.example.com" readOnly={settings.publicUrl.locked} {...bind("publicUrl")} />
            </SettingField>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("admin.settings.limits.title")}</CardTitle>
            <CardDescription>{t("admin.settings.limits.description")}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <SettingField
              id="install-limits-name"
              label={t("admin.settings.limits.name_label")}
              setting={settings.workspaceLimitsName}
            >
              <Input
                id="install-limits-name"
                aria-describedby={`${describedBy("install-limits-name", settings.workspaceLimitsName)["aria-describedby"]} install-limits-name-help`}
                maxLength={100}
                readOnly={settings.workspaceLimitsName.locked}
                {...bind("workspaceLimitsName")}
              />
              <p id="install-limits-name-help" className="text-xs text-muted-foreground">
                {t("admin.settings.limits.name_help")}
              </p>
            </SettingField>
            {DEFAULT_LIMITS.map((name) => {
              const id = `install-limit-${name}`;
              const setting = settings.workspaceLimits[name];
              return (
                <SettingField
                  key={name}
                  id={id}
                  label={t(`limits.names.${name}`)}
                  setting={setting}
                  error={errors.workspaceLimits?.[name]?.message}
                >
                  <Input
                    id={id}
                    {...describedBy(id, setting)}
                    type="number"
                    {...defaultLimitBounds(name, minCheckInterval)}
                    inputMode="numeric"
                    placeholder={t("limits.none")}
                    readOnly={setting.locked}
                    {...bindLimit(name)}
                  />
                </SettingField>
              );
            })}
          </CardContent>
        </Card>
      </fieldset>

      {saveError && (
        <Alert variant="destructive" role="alert">
          <AlertDescription>{saveError}</AlertDescription>
        </Alert>
      )}
      <div>
        <Button type="submit" disabled={!hasChanges || saving}>
          {saving ? t("common.saving") : t("common.save")}
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
// public URL, the default limits of a workspace, and the switches of the usage ping and error
// reports (docs/adr/0005, section 5)
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
