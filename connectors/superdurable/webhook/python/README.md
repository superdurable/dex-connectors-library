# Super Durable Webhook Python connector

The `dex-connector-superdurable-webhook` package implements `sendEvent` for
Python Dex Flows. It sends one JSON value to the connection's HTTPS
`deliveryUrl` with Standard Webhooks `webhook-id`, `webhook-timestamp`, and
`webhook-signature` headers. The same Dex call retains its webhook ID across
retries. The receiver must deduplicate that ID as described in the
[connector contract](../README.md#the-sendevent-mutation).

The Python package does not implement the Go `requestReceived` Trigger.
Configure `signing_secret` and `deliveryUrl` in Dex Web **Connections**, using
the [provider setup guide](../README.md#sender-setup). The secret never enters
Flow input or Results. A missing or invalid destination or payload selects
`defect`; a definite receiver rejection selects `rejected`. Both are optional
branches, so the [runnable Flow](examples/send_event/flow.py) leaves them
unwired and fails when selected.

Run the [send event example](examples/send_event/README.md) to exercise the
package through a Dex Worker and Client. Its static connection name is
`webhook-forwarding`. The example's `SendSignedWebhookEvent` Step exposes no
operation-specific configuration fields in Dex Web.
