import * as React from "react";
import { useFormContext, useFormState } from "react-hook-form";
import { Button } from "@/components/ui/button";
import { PasswordInput } from "@/components/ui/password-input";
import { Textarea } from "@/components/ui/textarea";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  REENTER_ERROR_TYPE,
  SECRET_PLACEHOLDER,
  configFieldKeys,
  askedToReenter,
  reenterError,
} from "../secrets";

type SavedSecretsContextValue = {
  // The secrets of the form that hold the placeholder though where the channel sends changed
  reenter: ReadonlySet<string>;
  // Tells that a secret field is on the form. Returns the call that tells it is gone.
  showSecret: (name: string) => () => void;
};

const SavedSecretsContext = React.createContext<SavedSecretsContextValue>({
  reenter: new Set(),
  showSecret: () => () => {},
});

function sameSet(a: ReadonlySet<string>, b: readonly string[]) {
  return a.size === b.length && b.every((key) => a.has(key));
}

// Wraps the fields of a channel type's form. The API shows a secret the channel has saved as
// SECRET_PLACEHOLDER, and the form holds it until the user types a new secret or removes it.
// The server keeps a saved secret only while the channel sends to the same place, so when an
// endpoint field moves away from `saved`, the secrets still holding the placeholder keep it but
// ask to be typed again or removed: their fields show a hint, and saving and testing are
// refused (see create-edit-notification-channel). Pass `saved` in the edit form: the values the
// form opened with.
//
// A secret whose field the form doesn't show, such as an ntfy token while it signs in with a
// password, can't be asked for. The form leaves it as it is, and sends it empty instead when it
// saves or tests with the endpoint moved (see withoutUnaskedSecrets). `shown` is the set of the
// secrets whose fields are on the form, which this keeps up to date; the parent reads it when
// it sends.
export function SavedSecrets({
  saved,
  shown,
  children,
}: {
  saved?: Record<string, unknown>;
  shown: React.MutableRefObject<Set<string>>;
  children: React.ReactNode;
}) {
  const { watch, getValues, getFieldState, setError, clearErrors } = useFormContext();
  const [reenter, setReenter] = React.useState<ReadonlySet<string>>(new Set());
  const [shownVersion, setShownVersion] = React.useState(0);

  const showSecret = React.useCallback(
    (name: string) => {
      shown.current.add(name);
      setShownVersion((version) => version + 1);
      return () => {
        shown.current.delete(name);
        setShownVersion((version) => version + 1);
      };
    },
    [shown]
  );

  const reconcile = React.useCallback(
    (values: Record<string, unknown>) => {
      if (!saved) return;
      const asked = askedToReenter(values, saved, shown.current);
      setReenter((current) => (sameSet(current, asked) ? current : new Set(asked)));

      // An error that asks for a secret or a URL goes once it is no longer asked for: it was
      // typed or removed, or the endpoint or URL is back as saved
      const type = typeof values.type === "string" ? values.type : "";
      const fields = configFieldKeys[type];
      for (const key of [...(fields?.secrets ?? []), ...(fields?.urls ?? [])]) {
        if (!asked.includes(key) && getFieldState(key).error?.type === REENTER_ERROR_TYPE) {
          clearErrors(key);
        }
      }

      // A URL has no hint of its own to show: it gets the error as soon as it is asked for,
      // rather than only when the form saves or tests
      for (const key of fields?.urls ?? []) {
        if (asked.includes(key) && getFieldState(key).error?.type !== REENTER_ERROR_TYPE) {
          setError(key, reenterError(type, key));
        }
      }
    },
    [saved, shown, getFieldState, setError, clearErrors]
  );

  React.useEffect(() => {
    if (!saved) return;
    const subscription = watch((values) => reconcile(values));
    return () => subscription.unsubscribe();
  }, [watch, saved, reconcile]);

  // A field coming or going changes which secrets can be asked for
  React.useEffect(() => {
    reconcile(getValues());
  }, [shownVersion, getValues, reconcile]);

  const value = React.useMemo(() => ({ reenter, showSecret }), [reenter, showSecret]);
  return (
    <SavedSecretsContext.Provider value={value}>{children}</SavedSecretsContext.Provider>
  );
}

// What a secret field shows while it holds a saved secret: nothing in the field, a hint under
// it, and a button that removes the secret.
function useSavedSecret(
  name: string | undefined,
  value: unknown,
  describedBy: string | undefined,
  id: string | undefined
) {
  const { t } = useLocalizedTranslation();
  const { reenter, showSecret } = React.useContext(SavedSecretsContext);
  const { setValue, getFieldState } = useFormContext();
  const formState = useFormState({ name });

  React.useEffect(() => (name ? showSecret(name) : undefined), [name, showSecret]);

  const saved = value === SECRET_PLACEHOLDER;
  let hint: string | null = null;
  if (saved) {
    hint =
      name && reenter.has(name)
        ? t("notifications.secret.reenter_hint")
        : t("notifications.secret.saved_hint");
  }
  // The field's error says the same, under the field
  if (name && getFieldState(name, formState).error?.type === REENTER_ERROR_TYPE) hint = null;

  const hintId = id ? `${id}-secret-hint` : undefined;
  const remove = () => {
    if (!name) return;
    // An explicit empty value: the server clears the secret
    setValue(name, "", { shouldDirty: true, shouldTouch: true });
    if (id) setTimeout(() => document.getElementById(id)?.focus(), 0);
  };
  const actions =
    hint || saved ? (
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {hint && (
          <p id={hintId} className="text-muted-foreground text-sm">
            {hint}
          </p>
        )}
        {saved && (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0"
            onClick={remove}
          >
            {t("notifications.secret.remove")}
          </Button>
        )}
      </div>
    ) : null;

  return {
    saved,
    actions,
    describedBy:
      [describedBy, hint ? hintId : undefined].filter(Boolean).join(" ") || undefined,
  };
}

// A password field for a secret of a notification channel. It shows empty while the form holds
// the placeholder of a saved secret: typing replaces it, leaving it keeps the saved secret, and
// Remove clears it. Emptying it after typing is a real clear too. Pass the field of a FormField,
// as for PasswordInput.
export function SecretInput({
  value,
  required,
  placeholder,
  ...props
}: React.ComponentProps<typeof PasswordInput>) {
  const { saved, actions, describedBy } = useSavedSecret(
    props.name,
    value,
    props["aria-describedby"],
    props.id
  );

  return (
    <>
      <PasswordInput
        {...props}
        value={saved ? "" : value}
        // A saved secret fills the field already, though it shows empty
        required={required && !saved}
        // "Leave empty to ..." would mislead: empty keeps the saved secret
        placeholder={saved ? undefined : placeholder}
        aria-describedby={describedBy}
      />
      {actions}
    </>
  );
}

// SecretInput for a secret that is longer text
export function SecretTextarea({
  value,
  placeholder,
  ...props
}: React.ComponentProps<typeof Textarea>) {
  const { saved, actions, describedBy } = useSavedSecret(
    props.name,
    value,
    props["aria-describedby"],
    props.id
  );

  return (
    <>
      <Textarea
        {...props}
        value={saved ? "" : value}
        placeholder={saved ? undefined : placeholder}
        aria-describedby={describedBy}
      />
      {actions}
    </>
  );
}
