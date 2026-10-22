import * as React from "react";
import { useFormContext } from "react-hook-form";
import { PasswordInput } from "@/components/ui/password-input";
import { Textarea } from "@/components/ui/textarea";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  SECRET_PLACEHOLDER,
  configFieldKeys,
  endpointChanged,
} from "../secrets";

// The secrets of the form that were emptied because where the channel sends changed
const ReenterSecretsContext = React.createContext<ReadonlySet<string>>(new Set());

// Wraps the fields of a channel type's form. The API shows a secret the channel has saved as
// SECRET_PLACEHOLDER, and the form holds it until the user types a new secret. The server keeps
// a saved secret only while the channel sends to the same place, so when an endpoint field
// moves away from `saved`, the secrets still holding the placeholder are emptied and the user
// types them again. Pass `saved` in the edit form: the values the form opened with.
export function SavedSecrets({
  saved,
  children,
}: {
  saved?: Record<string, unknown>;
  children: React.ReactNode;
}) {
  const { watch, setValue } = useFormContext();
  const [reenter, setReenter] = React.useState<ReadonlySet<string>>(new Set());

  React.useEffect(() => {
    if (!saved) return;
    const subscription = watch((values) => {
      const type = values.type as string;
      if (type !== saved.type) {
        // Another type has no saved secrets to ask for
        setReenter((current) => (current.size ? new Set() : current));
        return;
      }
      if (!endpointChanged(type, values, saved)) return;

      const emptied = (configFieldKeys[type]?.secrets ?? []).filter(
        (key) => values[key] === SECRET_PLACEHOLDER
      );
      if (emptied.length === 0) return;
      emptied.forEach((key) => setValue(key, ""));
      setReenter((current) => new Set([...current, ...emptied]));
    });
    return () => subscription.unsubscribe();
  }, [watch, setValue, saved]);

  return (
    <ReenterSecretsContext.Provider value={reenter}>
      {children}
    </ReenterSecretsContext.Provider>
  );
}

// What a secret field shows while it holds a saved secret, or has to be typed again: nothing
// in the field, and a hint under it.
function useSecretHint(
  name: string | undefined,
  value: unknown,
  describedBy: string | undefined,
  id: string | undefined
) {
  const { t } = useLocalizedTranslation();
  const reenter = React.useContext(ReenterSecretsContext);

  const saved = value === SECRET_PLACEHOLDER;
  let hint: string | null = null;
  if (saved) {
    hint = t("notifications.secret.saved_hint");
  } else if (value === "" && name && reenter.has(name)) {
    hint = t("notifications.secret.reenter_hint");
  }

  const hintId = id ? `${id}-secret-hint` : undefined;
  const hintElement = hint ? (
    <p id={hintId} className="text-muted-foreground text-sm">
      {hint}
    </p>
  ) : null;
  return {
    saved,
    hintElement,
    describedBy:
      [describedBy, hint ? hintId : undefined].filter(Boolean).join(" ") || undefined,
  };
}

// A password field for a secret of a notification channel. It shows empty while the form holds
// the placeholder of a saved secret: typing replaces it, and leaving it keeps the saved secret.
// Emptying it after typing is a real clear. Pass the field of a FormField, as for PasswordInput.
export function SecretInput({
  value,
  required,
  placeholder,
  ...props
}: React.ComponentProps<typeof PasswordInput>) {
  const { saved, hintElement, describedBy } = useSecretHint(
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
      {hintElement}
    </>
  );
}

// SecretInput for a secret that is longer text
export function SecretTextarea({
  value,
  placeholder,
  ...props
}: React.ComponentProps<typeof Textarea>) {
  const { saved, hintElement, describedBy } = useSecretHint(
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
      {hintElement}
    </>
  );
}
