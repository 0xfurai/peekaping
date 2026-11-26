import { z } from "zod";

const MIN_LENGTH = 8;

// The password rule of the server (validatePassword in apps/server/internal/validation), so a form
// accepts and rejects what the API does. The classes are the Unicode ones Go's unicode package
// uses: upper and lower case letters, numbers, and as special any character that is no letter
// and no number, a space too. The length counts characters (code points), not UTF-16 units or bytes.
// Change the two together.
export type PasswordProblem = "min_length" | "complexity";

export function passwordProblem(password: string): PasswordProblem | null {
  if ([...password].length < MIN_LENGTH) return "min_length";
  const hasAllClasses =
    /\p{Lu}/u.test(password) &&
    /\p{Ll}/u.test(password) &&
    /\p{N}/u.test(password) &&
    /[^\p{L}\p{N}]/u.test(password);
  return hasAllClasses ? null : "complexity";
}

// The schema of a password someone sets: signup, invite signup, reset and change
export const createNewPasswordSchema = (t: (key: string) => string) =>
  z.string().superRefine((password, ctx) => {
    const problem = passwordProblem(password);
    if (problem) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, message: t(`forms.validation.password_${problem}`) });
    }
  });
