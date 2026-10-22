---
sidebar_position: 8
---

# Usage statistics

Peekaping can send an anonymous usage ping: a short summary of how your install is set up. We use it to see which versions, modes, storage backends and monitor types are in use, so we know what is safe to change or remove.

**It is off until an admin turns it on.** A new install sends nothing. An install that upgrades from an older version stays off too.

## What is sent

A usage ping has these fields and no others:

| Field | Content |
| --- | --- |
| `instance_id` | A random ID that belongs to nothing about your install. See [Instance ID](#instance-id). |
| `version` | The Peekaping version, such as `0.0.48` |
| `mode` | `single` or `cluster`. See [Deployment modes](./deployment-modes.md). |
| `db` | The storage backend: `sqlite`, `postgres` or `mongo` |
| `os` | The operating system and CPU architecture, such as `linux/arm64` |
| `monitors` | How many monitors you have, paused ones included, as a range: `0`, `1-10`, `11-50`, `51-200`, `201-1000` or `1000+` |
| `monitor_types` | The monitor types you use, as names only, such as `http` or `docker`. Each type is listed once. |
| `notification_providers` | The notification channel types you use, as names only, such as `slack` or `telegram`. Each type is listed once. |
| `features` | Whether you have any status page (`status_pages`), any maintenance window (`maintenance`), and whether the metrics endpoint is on (`metrics`). Each is `true` or `false`. |

For example:

```json
{
  "instance_id": "0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50",
  "version": "0.0.48",
  "mode": "single",
  "db": "sqlite",
  "os": "linux/arm64",
  "monitors": "11-50",
  "monitor_types": ["docker", "http", "ping"],
  "notification_providers": ["slack", "telegram"],
  "features": {
    "status_pages": true,
    "maintenance": false,
    "metrics": true
  }
}
```

An install on any other storage backend isn't counted: the Worker rejects its usage ping and keeps nothing.

### Instance ID

The instance ID is a random UUID. Peekaping creates it when an admin turns the usage ping on and deletes it when they turn it off. If you turn the usage ping on again, you get a new ID. It is the only identifier in a usage ping, and nothing about your install goes into it.

The ID lets us count an install once instead of once a day. Because it changes when you opt out and in again, the counts are a floor, not a census.

### What is never sent

Nothing about individual monitors or channels is sent. A usage ping never has:

- a monitor name, URL, host, port or tag
- a status page name or address
- a notification channel setting, such as a webhook URL, a token or an email address
- a heartbeat or a response time
- your email address, your password or any other account data
- your own server's hostname

Counts are ranges, and types are names of kinds, never of single things.

## Why

Peekaping is self-hosted, so we can't see how it is used. Without the usage ping we don't know which versions are still in use, whether anyone runs cluster mode or MongoDB storage, or which monitor types matter. The usage ping is the only way we can tell.

## Where it goes and how long it is kept

The usage ping goes to a small Cloudflare Worker that the Peekaping project runs at `telemetry.peekaping.com`. Its source is in the [`apps/telemetry`](https://github.com/0xfurai/peekaping/tree/main/apps/telemetry) folder of the Peekaping repository.

The Worker accepts only the fields above and refuses anything else. It writes each valid usage ping to Cloudflare Workers Analytics Engine, which keeps the data for 3 months.

The Worker never reads or stores your IP address, the request headers, your country or any other detail of the request.

## How often

At most once a day. Peekaping checks a few minutes after it starts, and then every hour, whether a usage ping is due. A usage ping is due when the last one is more than 24 hours old. After an admin opts in, the first usage ping goes at the next check.

Only one process sends it. In [single mode](./deployment-modes.md#single-mode), that is the `peekaping` process. In [cluster mode](./deployment-modes.md#cluster-mode), it is one API replica, whichever claims the day's usage ping in Redis first. The producer, worker and ingester never send it.

A usage ping that fails to reach the Worker is dropped, not retried. A failure never affects your monitors or the API.

## Turn it on or off

Any of these turns it on or off for an admin:

- **The setup screen.** When you create the admin account, the box "Send anonymous usage statistics" is unticked. Tick it to opt in.
- **Settings.** The "Usage statistics" card has a switch. Turning it off deletes the instance ID, and turning it on creates a new one.

An environment variable switches it off whatever the setting says. While one applies, Peekaping sends nothing, and the switch in Settings is off and can't be changed:

| Variable | Value | Effect |
| --- | --- | --- |
| `TELEMETRY` | `off` (also `false`, `0` or `no`) | Turns the usage ping off |
| `DO_NOT_TRACK` | `1` (also `true`) | Turns the usage ping off. [Do Not Track](https://consoledonottrack.com/) is a convention many tools follow. |

Case doesn't matter. To be sure an install never sends, set the variable in your image or your compose file. In Docker Compose that is a line in the `.env` file:

```env
TELEMETRY=off
```

In Kubernetes, put `TELEMETRY: "off"` under `config.env` in your Helm values. See [Kubernetes (Helm)](./kubernetes-helm.md#configuration).

Set the variable on the `peekaping` container in single mode, and on every API replica in cluster mode. Those are the processes that send the usage ping and answer Settings.

## See exactly what is sent

The "Usage statistics" card in Settings shows the JSON the next usage ping would send. It comes from the same code that builds the usage ping, so it is exact. Before you opt in, `instance_id` is empty, and nothing is sent.
