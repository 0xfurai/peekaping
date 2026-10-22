---
sidebar_position: 8
---

# Usage statistics and error reporting

Peekaping can send two kinds of anonymous reports to the Peekaping project:

- **[Usage statistics](#usage-statistics)** say how your install is set up: the version, the mode, the storage backend and which kinds of monitors you use. We use them to see what is safe to change or remove.
- **[Error reports](#error-reporting)** say where Peekaping's own code crashed. We use them to find and fix bugs that show only on someone else's server.

**Both are off until an admin turns them on.** A new install sends nothing. An install that upgrades from an older version stays off too. Each one has its own box on the setup screen and its own switch in Settings, and turning one on does not turn on the other.

An environment variable switches either one off whatever the setting says. See [Turn it on or off](#turn-it-on-or-off) for the usage statistics and [Turn error reporting on or off](#turn-error-reporting-on-or-off) for the error reports.

## Usage statistics

Peekaping can send an anonymous usage ping: a short summary of how your install is set up. We use it to see which versions, modes, storage backends and monitor types are in use, so we know what is safe to change or remove.

### What is sent

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

#### Instance ID

The instance ID is a random UUID. Peekaping creates it when an admin turns the usage ping on and deletes it when they turn it off. If you turn the usage ping on again, you get a new ID. It is the only identifier in a usage ping, and nothing about your install goes into it.

The ID lets us count an install once instead of once a day. Because it changes when you opt out and in again, the counts are a floor, not a census.

#### What is never sent

Nothing about individual monitors or channels is sent. A usage ping never has:

- a monitor name, URL, host, port or tag
- a status page name or address
- a notification channel setting, such as a webhook URL, a token or an email address
- a heartbeat or a response time
- your email address, your password or any other account data
- your own server's hostname

Counts are ranges, and types are names of kinds, never of single things.

### Why

Peekaping is self-hosted, so we can't see how it is used. Without the usage ping we don't know which versions are still in use, whether anyone runs cluster mode or MongoDB storage, or which monitor types matter. The usage ping is the only way we can tell.

### Where it goes and how long it is kept

The usage ping goes to a small Cloudflare Worker that the Peekaping project runs at `telemetry.peekaping.com`. Its source is in the [`apps/telemetry`](https://github.com/0xfurai/peekaping/tree/main/apps/telemetry) folder of the Peekaping repository.

The Worker accepts only the fields above and refuses anything else. It writes each valid usage ping to Cloudflare Workers Analytics Engine, which keeps the data for 3 months.

The Worker never reads or stores your IP address, the request headers, your country or any other detail of the request.

### How often

At most once a day. Peekaping checks a few minutes after it starts, and then every hour, whether a usage ping is due. A usage ping is due when the last one is more than 24 hours old. After an admin opts in, the first usage ping goes at the next check.

Only one process sends it. In [single mode](./deployment-modes.md#single-mode), that is the `peekaping` process. In [cluster mode](./deployment-modes.md#cluster-mode), it is one API replica, whichever claims the day's usage ping in Redis first. The producer, worker and ingester never send it.

A usage ping that fails to reach the Worker is dropped, not retried. A failure never affects your monitors or the API.

### Turn it on or off

Any of these turns it on or off for an admin:

- **The setup screen.** When you create the admin account, the box "Send anonymous usage statistics" is unticked. Tick it to opt in.
- **Settings.** The "Usage statistics" card has a switch. Turning it off deletes the instance ID, and turning it on creates a new one.

An environment variable switches it off whatever the setting says. While one applies, Peekaping sends nothing, and the switch in Settings is off and can't be changed:

| Variable | Value | Effect |
| --- | --- | --- |
| `TELEMETRY` | `off` (also `false`, `0` or `no`) | Turns the usage ping off |
| `DO_NOT_TRACK` | `1` (also `true`) | Turns the usage ping off, and error reporting too. [Do Not Track](https://consoledonottrack.com/) is a convention many tools follow. |

`TELEMETRY=off` turns off only the usage ping. Case doesn't matter. To be sure an install never sends, set the variable in your image or your compose file. In Docker Compose that is a line in the `.env` file:

```env
TELEMETRY=off
```

In Kubernetes, put `TELEMETRY: "off"` under `config.env` in your Helm values. See [Kubernetes (Helm)](./kubernetes-helm.md#configuration).

Set the variable on the `peekaping` container in single mode, and on every API replica in cluster mode. Those are the processes that send the usage ping and answer Settings.

### See exactly what is sent

The "Usage statistics" card in Settings shows the JSON the next usage ping would send. It comes from the same code that builds the usage ping, so it is exact. Before you opt in, `instance_id` is empty, and nothing is sent.

## Error reporting

Peekaping can send a report when its own code crashes. The report says what kind of error it was and where in the code it happened, so we can find the bug and fix it. A crash on your server reaches us only if you file an issue.

**It is off until an admin turns it on**, and it is a separate choice from the usage statistics.

### When a report is sent

Only for a panic: a crash in Peekaping's own code, such as an index out of range or a nil pointer. Some panics are caught, and the request or check goes on. Others end a process, and that one is reported just before it stops.

Nothing else is reported. A failed check, a monitor that is Down, a wrong password, a bad request, a notification that a provider refuses and every other error that Peekaping handles and logs are normal, and none of them is a report. A mistake by a user is not a bug in Peekaping.

### What a report holds

A report has these fields and no others:

| Field | Content |
| --- | --- |
| Error type | The type of the panic value, such as `runtime.boundsError` or `*errors.errorString`. It names a kind of error and never holds its text. |
| Stack frames | For each frame, the function, the package, the file, the line and whether it is Peekaping's own code. The file is the package path and the file name, such as `peekaping/internal/httpx/middleware.go`, never a path on your machine. At most the 100 innermost frames. |
| Release | The Peekaping version, such as `0.0.48` |
| `mode` | `single` or `cluster`. See [Deployment modes](./deployment-modes.md). |
| `db` | The storage backend: `sqlite`, `postgres` or `mongo`. A cluster worker has no database settings, so it says `unknown`. |
| `os` | The operating system and CPU architecture, such as `linux/arm64` |
| `component` | The part of Peekaping that caught the panic, such as `http`, `executor` or `scheduler` |
| Level | `error` for a panic that was caught, `fatal` for one that ends the process |
| Bookkeeping | A random event ID, the time of the crash and the platform, `go`. The protocol needs them to accept the report. |

### What a report never holds

Peekaping builds each report from the list above, field by field. It copies nothing else, whatever the Sentry library or the code put on the event. A report never has:

- the error message or the value that was panicked with, which can hold a hostname or a URL
- breadcrumbs, the trail of events before the crash
- request data, such as a URL, a header, a cookie, a query string or a body
- local variables or function arguments
- the name of your server
- a user, an email address or any other account data
- the name, URL, host or setting of a monitor, status page or notification channel, such as a webhook URL

Peekaping adds no IP address to a report. The Sentry server sees the address a request comes from, as any server does, and the Sentry project that receives the reports is set not to store IP addresses.

### Where it goes

Reports go to a Sentry project that the Peekaping project runs. [Sentry](https://sentry.io) is an error tracking service. Peekaping sends them with Sentry's Go library.

The only link to Sentry is one setting, the DSN, which is the address of the project. The Peekaping project may move to a self-hosted [GlitchTip](https://glitchtip.com) later. That is a change of the DSN, and nothing in what is sent. You can set `ERROR_REPORTING_DSN` to send the reports to your own Sentry or GlitchTip instead. See [Turn error reporting on or off](#turn-error-reporting-on-or-off).

### How often reports are sent

One report from one place in the code per minute at most. A place is the part of Peekaping that caught the panic, the error type and the innermost stack frame. A check that panics on every run sends one report a minute, not one for each run. A panic that ends the process is always reported, and Peekaping waits up to 2 seconds for it to go out before the process stops.

A report that fails to reach Sentry is dropped, not retried. Sending a report never makes a check or a request wait for the network.

### Turn error reporting on or off

Any of these turns it on or off for an admin:

- **The setup screen.** When you create the admin account, the box "Send error reports" is unticked. Tick it to opt in. It is a different box from "Send anonymous usage statistics".
- **Settings.** The "Error reports" card has a switch.

An environment variable switches it off whatever the setting says. While one applies, Peekaping sends no reports, and the switch in Settings is off and can't be changed:

| Variable | Value | Effect |
| --- | --- | --- |
| `ERROR_REPORTING` | `off` (also `false`, `0` or `no`) | Turns error reporting off. The usage statistics are not affected. |
| `DO_NOT_TRACK` | `1` (also `true`) | Turns off both error reporting and the usage statistics |
| `ERROR_REPORTING_DSN` | A Sentry or GlitchTip DSN | Sends the reports to your own project instead of Peekaping's. A report holds the same fields, and every rule on this page still applies. |

`TELEMETRY=off` turns off only the usage statistics. Case doesn't matter. In Docker Compose, put the variable in the `.env` file:

```env
ERROR_REPORTING=off
```

In Kubernetes, put `ERROR_REPORTING: "off"` under `config.env` in your Helm values. See [Kubernetes (Helm)](./kubernetes-helm.md#configuration).

Set the variable on the `peekaping` container in single mode. In cluster mode, set it on every process, which means the API, the producer, the worker and the ingester, because each one reports the crashes in its own process. The `.env` file of the Compose stacks and `config.env` in Helm already reach all of them.

### In cluster mode

The API reads and changes the setting. It stores the choice and copies it to Redis, and the producer, worker and ingester read it from Redis once a minute. A change in Settings reaches every process within a minute, with no restart.
