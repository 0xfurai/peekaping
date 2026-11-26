import { isAxiosError } from "axios";

type Translate = (key: string, vars?: Record<string, unknown>) => string;

// The 409 for a user who is the only owner of workspaces (auth.LastOwnerErrorResponse): the code
// and the workspaces, as the admin's user delete answers it
const lastOwnerWorkspaces = (data: unknown): string[] | null => {
  const body = (data as { error?: { code?: unknown; workspaces?: unknown } } | undefined)?.error;
  if (body?.code !== "LAST_OWNER" || !Array.isArray(body.workspaces)) return null;
  return body.workspaces.map((workspace: { name?: unknown }) => String(workspace?.name ?? ""));
};

const serverMessage = (data: unknown): string => {
  const message = (data as { message?: unknown } | undefined)?.message;
  return typeof message === "string" ? message : "";
};

// The translation key of the refusals the server words in English (auth.controller, FailAccountRemoval)
const refusalKey = (status: number, text: string): string | undefined => {
  if (status === 409 && /at least one instance admin/i.test(text)) {
    return "security.delete_account.errors.last_instance_admin";
  }
  if (status === 400 && /password is incorrect/i.test(text)) {
    return "security.delete_account.errors.wrong_password";
  }
  return undefined;
};

/**
 * The text to show for a refused or failed deletion of the user's own account: a wrong password,
 * the workspaces they are the only owner of (to hand over or delete first), the last instance
 * admin; else the server's message, else the fallback.
 */
export const accountDeletionError = (error: unknown, t: Translate, fallback: string): string => {
  if (!isAxiosError(error) || !error.response) return fallback;
  const { status, data } = error.response;
  const names = status === 409 ? lastOwnerWorkspaces(data) : null;
  if (names) {
    return t("security.delete_account.errors.last_owner", {
      workspaces: names.map((name) => `"${name}"`).join(", "),
    });
  }
  const text = serverMessage(data);
  const key = refusalKey(status, text);
  return key ? t(key) : text || fallback;
};

/** What the delete-account card holds: the password typed, the open confirmation, the refusal shown. */
export type DeleteAccountForm = { password: string; confirming: boolean; error: string | null };

export type DeleteAccountAction =
  | { type: "typed"; password: string }
  | { type: "ask" }
  | { type: "cancel" }
  | { type: "refused"; error: string };

/**
 * The card's state after an action. A password isn't kept once the confirmation is cancelled or the
 * server refuses the deletion, so a refusal's message stays shown but the field is empty again.
 */
export const deleteAccountReducer = (form: DeleteAccountForm, action: DeleteAccountAction): DeleteAccountForm => {
  switch (action.type) {
    case "typed":
      return { ...form, password: action.password };
    case "ask":
      return { ...form, confirming: true, error: null };
    case "cancel":
      return { ...form, password: "", confirming: false };
    case "refused":
      return { password: "", confirming: false, error: action.error };
  }
};
