# Webhook Notifications

A webhook notification channel sends an HTTP `POST` request to your URL each time a monitor goes Down or comes back Up.

## Request body

Choose the body format in the **Request Body** field:

- **application/json**: a JSON object with `msg` (the notification text), `monitor` and `heartbeat`.
- **multipart/form-data**: a single form field, `data`, that holds the same JSON. In PHP, read it with `json_decode($_POST['data'])`.
- **Custom**: your own body, written as a [Liquid](https://shopify.github.io/liquid/) template and sent as `text/plain`. For example, `{{ monitor.name }}` and `{{ msg }}` insert the monitor name and the notification text.

**Additional Headers** takes a JSON object of header names and values, which are added to every request.

## Signing

Set a **Signing Secret** to sign every request with HMAC-SHA256, so your endpoint can check that a request came from Peekaping and wasn't changed on the way. Leave it empty to send unsigned requests.

A signed request carries two headers:

| Header                | Value                                                                                         |
| --------------------- | --------------------------------------------------------------------------------------------- |
| `X-Webhook-Timestamp` | When the request was sent, in Unix seconds, for example `1700000000`                          |
| `X-Webhook-Signature` | `sha256=` followed by the lowercase hex HMAC-SHA256 of the signed payload, keyed with the secret |

The signed payload is the timestamp, a dot, and then:

- the raw request body, for **application/json** and **Custom** bodies;
- the value of the `data` field, for **multipart/form-data**. Form parsers such as PHP's `$_POST` don't keep the raw multipart body, so the field is signed instead.

For example, with the timestamp `1700000000`, the body `hello` and the secret `key`, the signed payload is `1700000000.hello` and the header is:

```
X-Webhook-Signature: sha256=4d583a269f4f276a3fa80ff31b5a01879a848096983222a17893d198418939aa
```

Additional Headers can't replace `X-Webhook-Signature` or `X-Webhook-Timestamp`.

Signed requests don't follow redirects, since the redirect target could replay them. A `3xx` response counts as a failed send, so set the webhook URL to the final address.

## Verify the signature

1. Read `X-Webhook-Timestamp` and reject the request if it's more than a few minutes old.
2. Build the signed payload from the timestamp and the body exactly as received. Don't parse and re-encode the JSON first, since that can change its bytes.
3. Compute the HMAC-SHA256 of the payload with your secret, hex-encode it and add the `sha256=` prefix.
4. Compare the result with `X-Webhook-Signature` using a constant-time comparison.

The timestamp limits how long a captured request can be replayed, but it doesn't stop a replay within that window.

### Node.js (Express)

This example covers application/json and Custom bodies. For multipart/form-data, sign the `data` field from your form parser instead, as the Python and PHP examples do.

```js
import crypto from "node:crypto";
import express from "express";

const secret = process.env.WEBHOOK_SECRET;
const app = express();

// Keep the raw body of application/json and Custom requests for verification
app.post("/webhook", express.raw({ type: ["application/json", "text/plain"] }), (req, res) => {
  const timestamp = req.get("X-Webhook-Timestamp") ?? "";
  const signature = req.get("X-Webhook-Signature") ?? "";

  if (!/^\d+$/.test(timestamp) || Math.abs(Date.now() / 1000 - Number(timestamp)) > 300) {
    return res.status(401).send("Stale request");
  }

  const expected =
    "sha256=" +
    crypto.createHmac("sha256", secret).update(`${timestamp}.`).update(req.body).digest("hex");

  const given = Buffer.from(signature);
  const wanted = Buffer.from(expected);
  if (given.length !== wanted.length || !crypto.timingSafeEqual(given, wanted)) {
    return res.status(401).send("Invalid signature");
  }

  console.log(req.body.toString("utf8"));
  res.sendStatus(200);
});

app.listen(3000);
```

### Python (Flask)

```python
import hashlib
import hmac
import os
import time

from flask import Flask, abort, request

SECRET = os.environ["WEBHOOK_SECRET"].encode()
app = Flask(__name__)


@app.post("/webhook")
def webhook():
    timestamp = request.headers.get("X-Webhook-Timestamp", "")
    signature = request.headers.get("X-Webhook-Signature", "")

    if not timestamp.isdigit() or abs(time.time() - int(timestamp)) > 300:
        abort(401)

    if request.mimetype == "multipart/form-data":
        payload = request.form["data"].encode()
    else:
        payload = request.get_data()

    expected = "sha256=" + hmac.new(SECRET, timestamp.encode() + b"." + payload, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(signature, expected):
        abort(401)

    return "", 200
```

### PHP

```php
<?php
$secret = getenv('WEBHOOK_SECRET');
$timestamp = $_SERVER['HTTP_X_WEBHOOK_TIMESTAMP'] ?? '';
$signature = $_SERVER['HTTP_X_WEBHOOK_SIGNATURE'] ?? '';

if (!ctype_digit($timestamp) || abs(time() - (int) $timestamp) > 300) {
    http_response_code(401);
    exit('Stale request');
}

// multipart/form-data: the data field. application/json and Custom: the raw body.
$payload = $_POST['data'] ?? file_get_contents('php://input');

$expected = 'sha256=' . hash_hmac('sha256', $timestamp . '.' . $payload, $secret);
if (!hash_equals($expected, $signature)) {
    http_response_code(401);
    exit('Invalid signature');
}

// $payload is verified. It's JSON for application/json and multipart/form-data;
// a Custom body that isn't JSON stays as text.
$notification = json_decode($payload, true) ?? $payload;
```
