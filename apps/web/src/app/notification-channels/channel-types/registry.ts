import { z } from "zod";
import { smtpChannelType } from "./smtp";
import { telegramChannelType } from "./telegram";
import { webhookChannelType } from "./webhook";
import { slackChannelType } from "./slack";
import { ntfyChannelType } from "./ntfy";
import { pagerDutyChannelType } from "./pagerduty";
import { opsgenieChannelType } from "./opsgenie";
import { googleChatChannelType } from "./google-chat";
import { grafanaOncallChannelType } from "./grafana-oncall";
import { signalChannelType } from "./signal";
import { gotifyChannelType } from "./gotify";
import { pushoverChannelType } from "./pushover";
import { mattermostChannelType } from "./mattermost";
import { matrixChannelType } from "./matrix";
import { discordChannelType } from "./discord";
import { weComChannelType } from "./wecom";
import { whatsAppChannelType } from "./whatsapp";
import { twilioChannelType } from "./twilio";
import { sendGridChannelType } from "./sendgrid";
import { pushbulletChannelType } from "./pushbullet";
import { pagerTreeChannelType } from "./pagertree";
import { lineChannelType } from "./line";
import { teamsChannelType } from "./teams";

/**
 * Every notification channel type. This is the one list: the form schema, the form values type
 * and the select options come from it. The first entry is the type a new channel starts on. The
 * Type select lists the types by label instead (see `channelTypeOptions`), so the order here
 * doesn't show. To add a type, create its folder next to the others (schema.ts, form.tsx and an
 * index.ts with its definition; see docs/adr/0004-web-form-conventions.md) and add it here.
 */
export const channelTypes = [
  smtpChannelType,
  telegramChannelType,
  webhookChannelType,
  slackChannelType,
  ntfyChannelType,
  pagerDutyChannelType,
  opsgenieChannelType,
  googleChatChannelType,
  grafanaOncallChannelType,
  signalChannelType,
  gotifyChannelType,
  pushoverChannelType,
  mattermostChannelType,
  matrixChannelType,
  discordChannelType,
  weComChannelType,
  whatsAppChannelType,
  twilioChannelType,
  sendGridChannelType,
  pushbulletChannelType,
  pagerTreeChannelType,
  lineChannelType,
  teamsChannelType,
] as const;

type ChannelTypeDefinitions = (typeof channelTypes)[number];

export type ChannelType = ChannelTypeDefinitions["type"];

// The Type select's options, sorted by label with case ignored
export const channelTypeOptions = channelTypes
  .map((definition) => ({
    label: definition.displayName,
    value: definition.type,
  }))
  .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }));

type SchemasOf<Definitions extends readonly { schema: unknown }[]> = {
  -readonly [K in keyof Definitions]: Definitions[K] extends { schema: infer S } ? S : never;
};

// The schema of a channel's config: the `type` field picks the type's schema
export const channelConfigSchema = z.discriminatedUnion(
  "type",
  channelTypes.map((definition) => definition.schema) as SchemasOf<typeof channelTypes>
);

// The channel form: the name every channel has, plus the chosen type's config
export const channelFormSchema = z
  .object({
    name: z.string().min(1, {
      message: "Name is required",
    }),
  })
  .and(channelConfigSchema)
  .superRefine((values, ctx) => {
    const definition = getChannelType(values.type);
    // The union's `type` chose the definition, so its rules take these values
    (definition?.refine as ((values: unknown, ctx: z.RefinementCtx) => void) | undefined)?.(values, ctx);
  });

export type NotificationChannelFormValues = z.infer<typeof channelFormSchema>;

const byType = new Map<string, ChannelTypeDefinitions>(
  channelTypes.map((definition) => [definition.type, definition])
);

export const getChannelType = (type: string | undefined): ChannelTypeDefinitions | undefined =>
  type ? byType.get(type) : undefined;

export const isChannelType = (type: string): type is ChannelType => byType.has(type);
