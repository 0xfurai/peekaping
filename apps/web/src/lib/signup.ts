import { isAxiosError } from "axios";
import type { AuthAuthConfigResponse, AuthRegisterDto } from "@/api/types.gen";

type SignupConfig = Pick<AuthAuthConfigResponse, "signupOpen">;

/**
 * What the register route shows, by the install's /auth/config. `signupOpen` already covers the
 * first user of an invite install, so this never decides on the mode itself. A config that fails
 * to load sends the visitor to login, which offers nothing it can't deliver either.
 */
export const registerPageView = (
  config: SignupConfig | undefined,
  failed: boolean
): "loading" | "form" | "login" => {
  if (!config) return failed ? "login" : "loading";
  return config.signupOpen ? "form" : "login";
};

/**
 * What the login page says about creating an account: the register link when signup is open, the
 * way to an invite when it is not, and nothing while the config is unknown.
 */
export const loginSignupOffer = (config: SignupConfig | undefined): "register" | "ask_admin" | "none" => {
  if (!config) return "none";
  return config.signupOpen ? "register" : "ask_admin";
};

/**
 * The translation key of the register card's description. The install's first signup makes its
 * instance admin, in either mode (/auth/config says `firstSignup` while the install has no user);
 * a later signup in open mode is an ordinary account. With no mode known it reads as the admin one,
 * as an invite install's only signup is.
 */
export const registerDescriptionKey = (signupMode: "invite" | "open" | undefined, firstSignup: boolean | undefined): string =>
  signupMode === "open" && !firstSignup ? "auth.register.description_open" : "auth.register.description";

/**
 * Whether the register form shows the install-wide choices (the usage ping and the error reports).
 * Only an instance admin may set them, and only the install's first signup becomes one: the form
 * shows them exactly when /auth/config says `firstSignup`, in either mode. Later open-mode signups
 * and a config that is not loaded leave them to Settings.
 */
export const showInstallPreferences = (firstSignup: boolean | undefined): boolean => firstSignup === true;

/**
 * The register button waits for the widget's answer when the install uses Turnstile, and while a
 * request is pending, so that a second click can't send the signup twice.
 */
export const canSubmitRegister = (siteKey: string | null | undefined, token: string | null, pending = false): boolean =>
  !pending && (!siteKey || Boolean(token));

export type RegisterValues = { email: string; password: string; confirmPassword: string };

/** The request body: the form's values, with the Turnstile token when the widget gave one. */
export const registerBody = (values: RegisterValues, turnstileToken: string | null): AuthRegisterDto => ({
  ...values,
  ...(turnstileToken ? { turnstileToken } : {}),
});

/**
 * The translation key of a signup answer the visitor can act on, by its status and message
 * (auth.controller Register). undefined when the answer is any other, and the caller shows the
 * server's own message.
 */
export const registerFailureKey = (error: unknown): string | undefined => {
  if (!isAxiosError(error)) return undefined;
  const status = error.response?.status;
  const message = String(error.response?.data?.message ?? "").toLowerCase();
  if (status === 429) return "auth.register.errors.rate_limited";
  if (status === 409) return "auth.register.errors.email_taken";
  // Other 400s are rules the form shows as the server words them; only the Turnstile refusal is this
  if (status === 400 && message.includes("not a robot")) return "auth.register.errors.check_failed";
  return undefined;
};
