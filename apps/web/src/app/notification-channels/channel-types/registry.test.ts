import { describe, expect, it } from "vitest";
import { SECRET_PLACEHOLDER, configFieldKeys } from "../secrets";
import { channelTypeOptions, channelTypes, channelFormSchema, getChannelType, isChannelType } from "./registry";

describe("notification channel type registry", () => {
  it("lists each type once, under the type its schema declares", () => {
    const types = channelTypes.map((definition) => definition.type);
    expect(new Set(types).size).toBe(types.length);
    for (const definition of channelTypes) {
      expect(definition.schema.shape.type.value).toBe(definition.type);
      expect(definition.defaultValues.type).toBe(definition.type);
      expect(isChannelType(definition.type)).toBe(true);
      expect(getChannelType(definition.type)).toBe(definition);
    }
    expect(isChannelType("carrier-pigeon")).toBe(false);
  });

  it("lists the Type select's options alphabetically by label, ignoring case", () => {
    expect(channelTypeOptions.map((option) => option.label)).toEqual([
      "Discord",
      "Email (SMTP)",
      "Google Chat",
      "Gotify",
      "Grafana OnCall",
      "LINE messaging",
      "Matrix",
      "Mattermost",
      "Microsoft Teams",
      "NTFY",
      "Opsgenie",
      "PagerDuty",
      "PagerTree",
      "Pushbullet",
      "Pushover",
      "SendGrid",
      "Signal",
      "Slack",
      "Telegram",
      "Twilio",
      "Webhook",
      "WeCom (企业微信)",
      "WhatsApp (WAHA)",
    ]);
    // Each option carries the type of the definition it was made from
    for (const option of channelTypeOptions) {
      expect(getChannelType(option.value)?.displayName).toBe(option.label);
    }
  });

  it("starts a new channel on Email (SMTP)", () => {
    expect(channelTypes[0].type).toBe("smtp");
    expect(channelTypes[0].displayName).toBe("Email (SMTP)");
  });

  it("knows the secret and endpoint fields of every listed type", () => {
    for (const type of Object.keys(configFieldKeys)) {
      expect(isChannelType(type), `${type} has secrets but no channel type`).toBe(true);
    }
    for (const definition of channelTypes) {
      const keys = Object.keys(definition.schema.shape);
      const fields = configFieldKeys[definition.type];
      for (const key of [...(fields?.secrets ?? []), ...(fields?.endpoints ?? [])]) {
        expect(keys, `${definition.type}: ${key} is not a form field`).toContain(key);
      }
    }
  });

  describe.each(channelTypes.map((definition) => [definition.type, definition] as const))(
    "%s",
    (type, definition) => {
      it("accepts a saved channel whose secrets are the placeholder", () => {
        const secrets = configFieldKeys[type]?.secrets ?? [];
        const values: Record<string, unknown> = { name: "Channel", ...definition.defaultValues };
        for (const key of secrets) values[key] = SECRET_PLACEHOLDER;
        const result = channelFormSchema.safeParse(values);
        // A type with a required secret saves only with one: the placeholder stands for it
        if (secrets.length > 0) {
          const issues = result.success ? [] : result.error.issues.filter((issue) => secrets.includes(String(issue.path[0])));
          expect(issues, JSON.stringify(issues)).toEqual([]);
        }
        expect(result.success || result.error.issues.every((issue) => issue.path[0] !== "type")).toBe(true);
      });
    }
  );
});
