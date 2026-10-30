import { describe, expect, it } from "vitest";
import { createNewPasswordSchema } from "./password";

// The same cases as TestPasswordRule in apps/server/internal/validation/validation_test.go: the
// forms accept and reject what the server does
const cases: { name: string; password: string; problem: "min_length" | "complexity" | null }[] = [
  { name: "all classes and 8 characters", password: "Abcdef1!", problem: null },
  { name: "long passphrase", password: "Correct-horse-battery-1", problem: null },
  { name: "7 characters", password: "Abcde1!", problem: "min_length" },
  { name: "no uppercase letter", password: "abcdefg1!", problem: "complexity" },
  { name: "no lowercase letter", password: "ABCDEFG1!", problem: "complexity" },
  { name: "no number", password: "Abcdefgh!", problem: "complexity" },
  { name: "no special character", password: "Abcdefgh1", problem: "complexity" },
  { name: "empty", password: "", problem: "min_length" },
  { name: "tilde as the special character", password: "Abcdefg1~", problem: null },
  { name: "trailing space as the special character", password: "Abcdefg1 ", problem: null },
  { name: "apostrophe", password: "Abcdefg1'", problem: null },
  { name: "double quote", password: 'Abcdefg1"', problem: null },
  { name: "slash", password: "Abcdefg1/", problem: null },
  { name: "backslash", password: "Abcdefg1\\", problem: null },
  { name: "backtick", password: "Abcdefg1`", problem: null },
  { name: "7 characters, multi-byte letters with a digit and a special one", password: "Ééééé1!", problem: "min_length" },
  { name: "7 characters with a multi-byte special one", password: "Abcde1€", problem: "min_length" },
  { name: "8 characters with a multi-byte special one", password: "Abcdef1€", problem: null },
  { name: "8 characters, one an emoji outside the BMP", password: "Abcdef1😀", problem: null },
  { name: "7 characters, one an emoji outside the BMP", password: "Abcde1😀", problem: "min_length" },
  { name: "multi-byte upper and lower case letters count", password: "Éééééé1!", problem: null },
  { name: "multi-byte letters only, no number", password: "Éééééééé!", problem: "complexity" },
  { name: "other letters and a number are no special character", password: "Abcdéf1ß", problem: "complexity" },
];

describe("createNewPasswordSchema", () => {
  const schema = createNewPasswordSchema((key) => key);

  it.each(cases)("$name", ({ password, problem }) => {
    const result = schema.safeParse(password);
    if (problem === null) {
      expect(result.success).toBe(true);
      return;
    }
    expect(result.success).toBe(false);
    expect(result.error?.issues.map((issue) => issue.message)).toEqual([`forms.validation.password_${problem}`]);
  });
});
