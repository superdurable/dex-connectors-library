# Super Durable Webhook Python connector

The `dex-connector-superdurable-webhook` package implements `sendEvent` and
`requestReceived` for Python Dex Flows. It sends one JSON value to the connection's HTTPS
`deliveryUrl` with Standard Webhooks `webhook-id`, `webhook-timestamp`, and
`webhook-signature` headers. The same Dex call retains its webhook ID across
retries. The receiver must deduplicate that ID as described in the
[connector contract](../README.md#the-sendevent-mutation).

Configure `signing_secret` and `deliveryUrl` in Dex Web **Connections**, using
the [provider setup guide](../README.md#sender-setup). The secret never enters
Flow input or Results. An invalid destination or payload selects
`defect`; a definite receiver rejection selects `rejected`. Both are optional
branches, so the [runnable Flow](examples/send_event/flow.py) leaves them
unwired and fails when selected.

`SendEventInput.payload` accepts any JSON value, including `None` for JSON
`null`. Pass UTF-8 JSON `bytes` to send and sign the exact bytes supplied;
otherwise the connector encodes the Python value as JSON before checking
`maxBodyBytes`.

Run the [send event example](examples/send_event/README.md) to exercise the
package through a Dex Worker and Client. Its static connection name is
`webhook-forwarding`. The example's `SendSignedWebhookEvent` Step exposes no
operation-specific configuration fields in Dex Web.

## Receive webhooks in Python

The [`requestReceived` example](examples/request_received/README.md) runs an
ASGI endpoint and Dex Worker. It verifies HMAC-SHA256, Standard Webhooks, or a
shared token against `signing_secret`; accepts JSON and URL-encoded forms; and
uses `webhook-id`, `eventIdHeader`, `eventIdPointer`, then the body SHA-256 as
the stable event ID. Standard Webhooks timestamps outside
`timestampToleranceSeconds` are rejected. Invalid signatures or bodies return
400, oversized bodies 413, and a receiver without a running binding or
usable secret returns 503.

Each binding can filter with `matchPointer` and `matchValues`. The endpoint
acknowledges a filtered event with 200 without delivering it. Events copy only
`forwardedHeaders`; credentials, cookies, and signature headers are rejected
at configuration time. The project runtime writes every accepted event to its
durable inbox before returning 200 and replays pending events after restart.
The generated Python `WebhookRequestEvent.json_body` carries a decoded JSON
value; the raw bytes are used for signature verification and body-derived IDs.

The local integration test exercises a real Dex Worker and restart replay. A
live sender such as GitHub, Shopify, or Typeform has not been tested.
