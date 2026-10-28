import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  SECRET_PLACEHOLDER,
  configFieldKeys,
  secretsToReenter,
  withoutSavedSecrets,
  withoutUnaskedSecrets,
} from "./secrets";

const MASKED = "https://user:********@hooks.example.com/x?token=********";

describe("secretsToReenter: URL credentials", () => {
  it("returns a webhook URL edited while it still holds the placeholder", () => {
    const saved = { type: "webhook", webhook_url: MASKED };
    const values = { ...saved, webhook_url: "https://user:********@other.example.com/x?token=********" };
    expect(secretsToReenter(values, saved)).toEqual(["webhook_url"]);
  });

  it("returns nothing for a URL as shown", () => {
    const saved = { type: "webhook", webhook_url: MASKED };
    expect(secretsToReenter({ ...saved }, saved)).toEqual([]);
  });

  it("returns nothing for a URL retyped without the placeholder", () => {
    const saved = { type: "webhook", webhook_url: MASKED };
    const values = { ...saved, webhook_url: "https://user:pw@hooks.example.com/x?token=abc" };
    expect(secretsToReenter(values, saved)).toEqual([]);
  });

  it("returns a URL that is only a placeholder when it differs from what was shown", () => {
    const saved = { type: "webhook", webhook_url: MASKED };
    expect(secretsToReenter({ ...saved, webhook_url: SECRET_PLACEHOLDER }, saved)).toEqual([
      "webhook_url",
    ]);
  });

  it("returns a gotify token with its edited masked server URL", () => {
    const saved = {
      type: "gotify",
      server_url: "https://u:********@gotify.example.com",
      application_token: SECRET_PLACEHOLDER,
    };
    const values = { ...saved, server_url: "https://u:********@other.example.com" };
    expect(secretsToReenter(values, saved).sort()).toEqual(["application_token", "server_url"]);
  });

  it("asks for a Signal URL, which has no secrets, when it is edited while masked", () => {
    const saved = { type: "signal", signal_url: "https://********@signal.example.com/" };
    expect(secretsToReenter({ ...saved }, saved)).toEqual([]);
    const values = { ...saved, signal_url: "https://********@other.example.com/" };
    expect(secretsToReenter(values, saved)).toEqual(["signal_url"]);
  });

  it("asks for nothing once the type is not the saved one", () => {
    const saved = { type: "webhook", webhook_url: MASKED };
    expect(secretsToReenter({ type: "ntfy", webhook_url: "https://********@x/" }, saved)).toEqual([]);
  });
});

describe("withoutSavedSecrets", () => {
  it("empties a placeholder and a masked URL", () => {
    expect(
      withoutSavedSecrets({ type: "webhook", webhook_url: MASKED, name: "n", token: SECRET_PLACEHOLDER })
    ).toEqual({ type: "webhook", webhook_url: "", name: "n", token: "" });
  });

  it("keeps other text that merely contains the placeholder", () => {
    const body = '{"text": "********"}';
    expect(withoutSavedSecrets({ webhook_custom_body: body, name: "a ******** b" })).toEqual({
      webhook_custom_body: body,
      name: "a ******** b",
    });
  });
});

describe("withoutUnaskedSecrets", () => {
  it("never blanks a URL, shown or not", () => {
    const saved = { type: "gotify", server_url: "https://u:********@a.example.com", application_token: SECRET_PLACEHOLDER };
    const values = { ...saved, server_url: "https://u:********@b.example.com" };
    expect(withoutUnaskedSecrets(values, saved, new Set())).toEqual({
      ...values,
      application_token: "",
    });
  });
});

describe("configFieldKeys urls", () => {
  // The `urlCredentials:"true"` tags on the provider config structs are the source
  function serverUrlFields(): Record<string, string[]> {
    const dir = join(process.cwd(), "../server/internal/modules/notification_channel/providers");
    const fields: Record<string, string[]> = {};
    for (const file of readdirSync(dir)) {
      if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
      for (const line of readFileSync(join(dir, file), "utf8").split("\n")) {
        const key = /json:"([^",]+)[^"]*"/.exec(line)?.[1];
        if (key && line.includes('urlCredentials:"true"')) {
          const type = file.replace(/\.go$/, "");
          (fields[type] ??= []).push(key);
        }
      }
    }
    return fields;
  }

  it("lists the fields the server tags urlCredentials", () => {
    const fromWeb = Object.fromEntries(
      Object.entries(configFieldKeys)
        .filter(([, keys]) => keys.urls.length > 0)
        .map(([type, keys]) => [type, [...keys.urls]])
    );
    expect(fromWeb).toEqual(serverUrlFields());
    expect(fromWeb).toEqual({
      gotify: ["server_url"],
      matrix: ["homeserver_url"],
      ntfy: ["server_url"],
      pagerduty: ["pagerduty_integration_url"],
      signal: ["signal_url"],
      telegram: ["server_url"],
      webhook: ["webhook_url"],
      whatsapp: ["server_url"],
    });
  });
});
