# Twilio text message delivery example

This example sends one text message from a Flow started in Dex Web **Start
Flow**, then follows it to a final delivery status:

1. `RecordTextMessageRequest` validates and stores the request.
2. `SendTextMessage` runs `sendMessage` once.
3. On `accepted`, `WaitForDeliveryStatus` waits on a durable Timer and
   `GetTextMessage` runs `getMessage`. `RecordDeliveryStatus` completes on
   `delivered`, `read`, `undelivered`, `failed`, or `canceled`, and completes
   as `deliveryStatusUnconfirmed` after the last allowed read. The Worker
   reads every 15 seconds, at most 8 times.
4. On `providerRejected`, the Flow completes as `rejected` with Twilio's error
   code, such as 21608 for an unverified trial recipient.
5. On `uncertain`, the Flow records the Call ID and waits for an operator.
   It never sends the text again on its own.

## Reconcile an uncertain send

Twilio has no idempotency key, so an unknown outcome may still have sent the
text. The `twilio-text-message-phase` Attribute is then `needsReconciliation`,
and Dex Web offers two Actions that require the
`twilio-text-message.reconcile` permission:

- **Confirm sent message** takes the `SM` or `MM` SID the operator found in
  Twilio Console > Monitor > Logs > Messaging, filtered by the recipient and
  the recorded time. The Flow reads it with `getMessage` and adopts it only
  when its recipient matches the request; an unknown SID or another recipient
  returns the Flow to `needsReconciliation` with a note.
- **Resend message** is the only path that sends again. It runs
  `SendTextMessage` as a new Step execution with a new Call ID.

Other unwired optional branches, such as `defect`, fail the Flow.

## Run

Generate strict FDG 2.0 from `connectors/twilio/messaging` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/text-message-delivery/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/text-message-delivery
```

The graph is valid; before the first connector release it reports only the
expected `connector_release_required` warning on both Twilio Steps.

Run `dexcli dev` with that build directory, open **Connections**, and
configure `twilio-messaging / twilio-messaging` with either authentication
method. Dex Web or Superverse Studio writes the connection to the project
configuration; start the Worker with the `DEX_PROJECT_*` environment that names
it, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/text-message-delivery
```

The default Worker address is `127.0.0.1:8827`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `TwilioTextMessageDelivery` with a unique Flow ID. A blank `sender` uses
the connection's default sender:

```json
{"to": "+14155550100", "body": "Your table is ready.", "sender": ""}
```

A live send costs money and reaches a real phone. On a trial account, send
only to a Verified Caller ID.

## Test

```bash
GOWORK=off go test -race ./examples/text-message-delivery/...
```

With a Dex development server running, the integration tests start a real
Worker and Client against a deterministic Twilio fake:

```bash
GOWORK=off go test -tags=integration ./examples/text-message-delivery/... -count=1 -v
```

They cover a delivered text read twice through the Timer, a 21608 rejection,
a 429 retried after `Retry-After`, a timeout after dispatch reconciled through
an unknown SID, another recipient's SID, and the created SID with exactly one
create request, a nine-second dispatch that outlasts Dex's async local phase
and is still sent once, a 5xx resent only after **Resend message**, and an
unconfirmed delivery that stops after the last read. Set `DEX_FLOW_SERVICE_ADDRESS` when
the server is not at `127.0.0.1:8801`.
