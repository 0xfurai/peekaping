# 0004: Web form conventions

Status: Accepted
Date: 2026-10-22

## Context

The web app builds its forms with react-hook-form, a zod schema per form and the shadcn form primitives. The API stores each monitor's and each notification channel's settings as one opaque JSON string, and the OpenAPI spec says nothing about its shape, so the web app owns the shape of every type's config by hand.

Before this decision the monitor types used three layouts at once (schema, codec and component in one 500-line file, or a split `schema.ts`, or a mix), the names of the same things differed from type to type (`TCPForm` was both a values type and a component; `XForm`, `XFormValues`, `XFormData` and `FormValues` all meant form values), and adding a monitor type meant editing four hand-kept lists. The common request-body fields were copied into 16 serializers, and each type's form reset itself on mount, which wiped a cloned monitor's values. Nothing tested a codec.

## Decision

### 1. A type is a folder

Each monitor type and each notification channel type is one kebab-case folder under `monitors/monitor-types/` or `notification-channels/channel-types/`:

| File | Holds | Exports |
|---|---|---|
| `schema.ts` | the zod schema, the values type and the defaults | `xSchema`, `XFormValues`, `xDefaultValues` |
| `codec.ts` (monitors) | the wire config type and the conversion to and from the API | `XConfig`, `serialize`, `deserialize`, `formatTarget` |
| `form.tsx` | the type's fields, nothing else: no form element, button or reset | `XForm` as the default export |
| `index.ts` | the definition the registry lists | `xMonitorType` or `xChannelType` |
| `<section>.tsx` | a section of a long form, a plain component | the section component |

Form files export only components; the lint rule `react-refresh/only-export-components` enforces it in these folders. A section's schema lives in the type's `schema.ts`.

Shared monitor sections live in `monitor-types/shared/`: one `schema.ts` with every section's schema, `monitorBaseSchema` (the fields all types have) and its defaults, one `codec.ts` with `serializeMonitorBase` and `deserializeMonitorBase`, and one component file per section. A type's schema is `z.object({ type: z.literal("x"), ...own fields }).merge(monitorBaseSchema)`, plus `proxiesSchema` if it uses a proxy. Its codec spreads the base codec and adds `type` and `config`.

### 2. The registry is the one list

`monitor-types/registry.ts` and `channel-types/registry.ts` each hold one `as const` array of definitions. The monitor Type select shows them in that order; the channel Type select sorts them by label (`channelTypeOptions`), and the first channel type is the one a new channel starts on. The form schema (a discriminated union on `type`), the form values type, the select options, the lookup by type, cloning and the monitor page's target all derive from it. Adding a type is creating its folder and adding one line.

The wrapper (`components/create-edit-form.tsx`) owns what every type's form used to copy: the form element, the submit handler (through `serializeMonitor`) and the submit button. The general section's Type select switches type: a new monitor that changes type starts from the new type's defaults, keeping the name typed so far. The reset happens in the select's change handler, before the new type's form mounts, so what that form's own effects set (a push token) stays. The values the form opened with, including a clone's, are left alone, and an edited monitor keeps its values when its type changes.

### 3. Names

- Form values: `XFormValues`. Never `XForm`, which is the component, and never `XFormData`, `XFormType` or a bare `FormValues`.
- Schema: `xSchema`. Defaults: `xDefaultValues`. Wire config: `XConfig`.
- Codec functions are `serialize`, `deserialize` and `formatTarget`, unqualified: each lives in its type's folder and only the type's `index.ts` imports them.
- Acronyms are words: `Http`, `Tcp`, `Dns`, `Smtp`, `Mqtt`, `Grpc`, `Api`, `TwoFa`. Product names keep their own spelling: `MongoDB`, `MySQL`, `RabbitMQ`, `PagerDuty`, `WeCom`.
- Files are kebab-case.

A standalone form (proxy, tag, status page, maintenance, login, API key) keeps its schema at the top of its own file under the same names, and exports the values type when a caller needs it.

### 4. Numbers

A numeric field's schema coerces: `z.coerce.number()`, with the input left as `type="number"` and `{...field}`. No `onChange` that only parses the value by hand. The SMTP settings keep theirs: they reject out-of-range keystrokes as they are typed and map an empty optional timeout to unset, which coercion can't express.

### 5. Codecs are tested

`registry.test.ts` beside each registry checks every listed type: its `type` matches its schema's literal, its defaults serialize to a request body whose config is JSON and read back as the same values, and the registry serializes by the values' type. Every secret and endpoint field `secrets.ts` names for a channel type must be a field of that type's schema. Run with `bun run test` in `apps/web`; CI runs it between lint and build.

## Consequences

- Every monitor type is 3 or 4 small files instead of one. The HTTP types share `http/schema.ts` and `http/codec.ts` for their sections and header handling.
- The type forms import the form context, and the registry imports the type forms, so the registry's exports are read inside components and hooks, never at a module's top level. `monitor-form-context.tsx` reads the default type inside the provider for this reason.
- `proxy_id` is `""` when a monitor has no proxy, in defaults as well as in saved monitors; that is what the API stores and what removing a proxy sets.
- Saved `0` values for retries and the resend interval load as `0`. Five types used to turn them into the fallback.
- The OpenAPI spec could carry the per-type config structs so that `XConfig` is generated rather than hand-written. Not done here; it would remove the last hand-kept copy of a server struct.
