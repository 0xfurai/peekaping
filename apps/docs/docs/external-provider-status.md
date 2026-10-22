---
sidebar_position: 5
---

# Monitor external cloud and SaaS providers

Peekaping can monitor the status that an external provider publishes about
itself. This is useful when you depend on services such as AWS, Google Cloud,
Azure, Cloudflare, or GitHub and want provider incidents to use the same
history, notifications, metrics, and status pages as your other monitors.

Provider status is explanatory evidence. Keep a synthetic monitor for the
endpoint your application actually uses: a public status feed can lag, omit
tenant-specific failures, or report a broad incident that does not affect you.

## Create a JSON Query monitor

[OutageDeck](https://outagedeck.com/developers/api) normalizes official status
feeds behind one public JSON contract. Create an **HTTP(s) - Json Query**
monitor with these values:

| Field | Value |
| --- | --- |
| URL | `https://outagedeck.com/api/v1/providers/<slug>` |
| Method | `GET` |
| Accepted status codes | `2XX` |
| JSON Query Expression | `data.currentStatus.code` |
| Condition | `==` |
| Expected value | `operational` |
| Interval | `600` seconds |

Replace `<slug>` with the provider you need. Common examples are:

| Provider | Slug |
| --- | --- |
| AWS | `aws` |
| Google Cloud | `google-cloud` |
| Microsoft Azure | `azure` |
| Cloudflare | `cloudflare` |
| GitHub | `github` |

For example, an AWS monitor uses:

```text
https://outagedeck.com/api/v1/providers/aws
```

No API key is required for this monitor. The ten-minute interval stays within
the anonymous quota and matches the cache cadence of the underlying official
feeds.

## Understand the result

The configuration above is intentionally binary:

- `operational` makes the monitor **Up**.
- `degraded`, `partial_outage`, `major_outage`, `maintenance`, or `unknown`
  makes the monitor **Down**.
- An unavailable response or a missing JSON path also makes the check fail.

Use retries before marking the monitor down if you do not want a single fetch
failure to create an incident. The response also includes source freshness,
service-level rows, active incidents, and links to the official source for
future, more granular checks.

For a useful status page, publish the provider-status monitor next to your
synthetic application monitor. That keeps “the provider reports a problem”
separate from “our service is failing” instead of treating either signal as
proof of the other.
