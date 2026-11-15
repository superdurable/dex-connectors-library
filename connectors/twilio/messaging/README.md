# Twilio Messaging Connector

Moved from `github.com/superdurable/dex-connectors-library/connectors/twilio/messaging`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Twilio; no live Twilio account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module sends SMS, MMS, and WhatsApp messages through Twilio Programmable
Messaging, reads their delivery status, and receives inbound messages:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go@v0.24.0
```

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `sendMessage` | Mutation | `accepted` | `providerRejected`, `uncertain`, `defect` |
| `getMessage` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

It also exposes one Trigger:

- `messageReceived` delivers each inbound SMS, MMS, or WhatsApp message to the
  account, or to one Twilio number, read by polling the Messages list. See
  [messageReceived Trigger](#messagereceived-trigger).

Only a safe message view crosses the connector boundary: the message SID,
account and Messaging Service SIDs, sender, recipient, Twilio's own status
value, error code, segment and media counts, price, and timestamps. Results,
receipts, failures, and logs never contain the message body, media URLs,
Twilio's error message text, the Auth Token, or the API key secret. The
`messageReceived` Trigger event is the one exception for the body: it carries
the inbound text, because acting on an inbound message needs it.

## Twilio setup

Every connection needs the **Account SID** (`AC` followed by 32 hexadecimal
characters) from the [Twilio Console](https://console.twilio.com/) Account
Dashboard. Use the SID of the subaccount that owns the sender when the sender
belongs to a subaccount. Then choose one authentication method:

- **Account SID and Auth Token** (`auth-token`, the default) authenticates as
  the account with its primary Auth Token from the same Account Info panel.
  The token grants full account access.
- **API key** (`api-key`, recommended) authenticates with an API key SID
  (`SK...`) and its secret, created under Account > API keys & tokens. The
  secret is shown once. A Restricted key can be limited to creating and reading
  Messages, and deleting the key revokes only this connection.

The **default sender** is optional. It is an SMS-capable Twilio number in
E.164 form, a WhatsApp sender written `whatsapp:+14155550100`, or a Messaging
Service SID (`MG...`). Short codes and alphanumeric sender IDs are used through
a Messaging Service. A blank default means every send names its own `sender`.

Trial accounts can send only to numbers listed under Phone Numbers > Manage >
Verified Caller IDs; other recipients fail with Twilio error 21608, which
selects `providerRejected`. Twilio's test credentials cannot read messages, so
`getMessage` selects `providerRejected` with them; use live credentials.

Credentials, including the API key SID, are reread before every provider call,
so replacing a key in Dex Web takes effect without a restart. The Account SID,
default sender, and endpoint are startup configuration.

### No sender or number picker yet

The default sender is a guided text field, and the `receivingAddressInput`
Studio unit of the `messageReceived` Trigger is a guided, validated text input,
rather than pickers built from the account's IncomingPhoneNumbers and
Messaging Services. The `ui/` Studio bundle declares no setup commands: Twilio's
REST API accepts only HTTP Basic authentication,
while Studio setup commands support only a `bearer` credential or a raw secret
in a named header, and `Authorization` is a reserved header. A picker therefore
cannot authenticate until the manifest schema and Dex Web gain a Basic
credential scheme that pairs a non-secret username with a secret field. The
bundle never receives credentials; `connectorctl ui-artifact` packages its
build as the release's Studio artifact.

Dex Web `cli-v1.1.0` also omits auth-method `configuration.fields` from the
setup form, so the API key SID is a non-secret credential field of the
`api-key` method instead of method configuration.

## Project configuration

Dex Web or Superverse Studio writes the connection to the project
configuration. Load it with `projectconfig.LoadFromEnvironment` and open the
connection by the name the application uses with
`messaging.NewProjectConnection`, as
[`examples/text-message-delivery/main.go`](go/examples/text-message-delivery/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := messaging.NewProjectConnection(project, textmessagedelivery.ConnectionName)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds the selected `auth_method` with `auth_token`, or
with `api_key_sid` and `api_key_secret`.

## Operations

### sendMessage

`SendMessageInput` names the recipient `to` (E.164, or `whatsapp:` plus
E.164), a `body` of at most 1600 characters, up to 10 public HTTPS
`mediaUrls`, and an optional `sender` that overrides the default sender. A
WhatsApp recipient needs a WhatsApp sender or a Messaging Service. Invalid
input selects `defect` without calling Twilio.

Twilio's [Messages API](https://www.twilio.com/docs/messaging/api/message-resource)
has no documented idempotency key or lookup by connector Call ID. The shared
single-dispatch fence permits a retry only after a saved rejection or positive
proof that no connection opened. Its checkpoint is read before credentials or
provider lookups, and a missing or stale checkpoint forbids another send.

| Outcome | Result |
| --- | --- |
| 2xx with a valid message | `accepted`, with the SID and status, usually `queued` or `accepted` |
| 400, 401, 403, 404, or another 4xx except 408, 409, and 429 | `providerRejected`, with the Twilio error code in `Value.ErrorCode` and the Receipt's `twilioErrorCode`; a 401 retries only if a fresh credential differs |
| 429 or 503 | Retry, after `Retry-After` when Twilio sends it |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 408, 409, 3xx, or 5xx other than 503 after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, for another account, or reflects the credential | `uncertain` |

Twilio documents that [429 requests are not processed](https://www.twilio.com/docs/api/errors/20429)
and [503 means temporarily unavailable and should be retried](https://www.twilio.com/docs/usage/twilios-response).
Its Messages documentation does not specify a safe retry for 409, so a 409
without a message SID requires reconciliation. The fence bounds the POST to
7/12 of the smaller 30-second Execute timeout and remaining Step deadline,
leaving time to classify its result. It disables Go's transparent HTTP replay.
A transport without positive DNS, connect, or TLS failure evidence is treated
as possibly dispatched.
An unavailable credential service retries before the POST with a `notSent`
checkpoint. An invalid or reauthorization-required credential selects `defect`
without sending and asks for the connection to be updated or reconnected. On
401, the connector requests a forced credential refresh;
it records a safe retry only if that returns a different valid credential.
Static Twilio credentials cannot refresh automatically, so a 401 selects
`providerRejected` with an update-connection hint until the connection is
updated.

After a valid 2xx answer, the connector saves the message SID. If Dex repeats
that Step before committing its result, the next attempt reads the saved SID
with `getMessage` and never sends a second POST. A transient read-back failure
retries the read. Reauthorization-required credentials, a missing SID, and
unconfirmable results select `uncertain`, with the saved SID in the receipt
when available. The heartbeat frame may still be lost before Dex persists it;
the fence then fails closed and the operator reconciles the send.

`uncertain` means the message may exist. On `providerRejected` and
`uncertain`, `Value` has no SID and echoes the requested account, sender, and
recipient so the application can reconcile.

### getMessage

`GetMessageInput.MessageSID` is the `SM` or `MM` SID from `accepted`. `found`
returns the current status verbatim, including values Twilio adds later.
`sent` can be the last status for destinations that return no delivery
receipt; `undelivered` and `failed` carry the error code. A missing message is
`notFound`. Transport failures, 408, 429, and 5xx retry under the generated Dex
policy; a malformed, oversized, or mismatched response is `invalidResponse`.

## Avoiding duplicate texts

Before upgrading from v0.21.x, reconcile in-flight `sendMessage` calls that are
retrying after 429 or a failed connection. Those attempts have no dispatch
checkpoint. A later attempt under v0.22.0 fails closed as `uncertain` even when
the earlier request never reached Twilio.

`sendMessage` uses sync Execute durability because later attempts need its
single-dispatch heartbeat checkpoint. Async local-phase heartbeats are not
visible to those attempts. `getMessage` is a read and keeps the async default.
Do not override `sendMessage` to async.

An application handles `uncertain` without resending automatically.
[`examples/text-message-delivery`](go/examples/text-message-delivery) shows the
pattern:

1. Record the request in an Attribute before the send Step.
2. Wire `uncertain` to a Step that records the connector Call ID and the time
   the outcome was observed, then leaves the Flow waiting.
3. An operator searches Twilio Console > Monitor > Logs > Messaging for the
   recipient around that time. The **Confirm sent message** Action reads the
   reported SID with `getMessage` and adopts it only when its recipient matches.
   The **Resend message** Action is the only path that sends again, as a new
   Step execution with a new Call ID.

The Twilio Message resource has no metadata field and its list filters only by
`To`, `From`, and send date, so a Step-derived marker cannot be stored on the
message itself. Automated reconciliation is planned for later releases: a
`listMessages` Query filtered by recipient, sender, and send date, and a
status-callback Trigger whose callback URL carries the Call ID.

## Mocks

The generated `messagingmock` package provides one consistent default world:
`sendMessage` queues a text to `+14155550123` from `+14155550100`, and
`getMessage` reads that SID as delivered. It also scripts a rejected trial
recipient and `getMessage` not-found, provider-rejected, and invalid-response
outcomes. Other recipients, senders, or SIDs need explicit scripted cases.
The runnable example tests its own Flow on the defaults:

```go
mock := messagingmock.New(t, ConnectionName)
flow, harness := newTwilioIntegrationHarnessForConnection(t, mock.Connection())
ctx := integrationContext(t)
input := Input{To: "+14155550123", Body: "Your table is ready."}
flowID := startTextMessageFlow(t, ctx, harness, flow, "mock-default", input)
delivery := waitForFlowOutput(t, ctx, harness, flowID)

require.Equal(t, PhaseFinished, delivery.Phase)
require.Equal(t, input, delivery.Request)
require.Equal(t, 1, delivery.SendAttempts)
require.Equal(t, 1, delivery.StatusChecks)
require.Nil(t, delivery.UncertainSend)

sends := mock.SendMessage().Calls()
require.Len(t, sends, 1)
require.Equal(t, messaging.SendMessageInput{To: "+14155550123", Body: "Your table is ready."}, sends[0].Input)
reads := mock.GetMessage().Calls()
require.Len(t, reads, 1)
require.Equal(t, messaging.GetMessageInput{MessageSID: "SMfedcba9876543210fedcba9876543210"}, reads[0].Input)
```

The send Step wiring in the example:

```go
dex.DefineStep(messaging.NewSendMessageStep(messaging.SendMessageStepConfig[Input]{
	StepType: sendTextMessageStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "twilio", GroupLabel: "Twilio",
		Explanation: "Send the text once; an unknown outcome is reconciled, never resent automatically.",
	},
	Connection: flow.connection, MapToOperationInput: flow.MapToSendMessageInput,
	Accepted: sdkgo.GoTo(recordAcceptedTextMessage{}),
	// An uncertain send continues through operator reconciliation Actions.
	Uncertain: sdkgo.GoTo(recordUncertainTextMessage{}),
})),
```

## messageReceived Trigger

`messageReceived` polls the account's
[Messages list](https://www.twilio.com/docs/messaging/api/message-resource#read-multiple-message-resources)
and delivers one `messaging.MessageReceivedEvent` per inbound message, with
the safe message view and the text. It is built on the Connector SDK's durable
poll Trigger kit ([`sdkgo/pollingtrigger`](../../../sdkgo/README.md#polling-triggers),
sdkgo v0.38.0). Twilio offers no change feed or cursor token, so the binding's
cursor is a `date_sent` watermark plus a snapshot of the message SIDs it
delivered within the last two minutes.

- **Binding.** The `receivingAddressInput` unit saves `to`: a Twilio number in
  E.164 form or a `whatsapp:+E.164` sender, sent as the list's `To` filter.
  Blank receives every inbound message of the account. Outbound messages are
  listed but never delivered.
- **Baseline.** The first poll lists the last two minutes and records those
  inbound messages as delivered. Existing messages are never delivered.
- **Polls.** Each poll lists `DateSent>=` the watermark minus two minutes, so a
  message Twilio dates slightly late is still found, and skips SIDs already in
  the snapshot. It delivers at most 500 messages per page, oldest first, then
  moves the watermark to the newest listed `date_sent`.
- **Held messages.** An inbound message still in `receiving`, such as an MMS
  whose media Twilio is fetching, is delivered once it is `received`. It holds
  the watermark back for at most ten minutes, then is delivered as it is. A
  listed message without `date_sent` holds the watermark at its current value.
- **Pagination.** Each request asks for 200 messages and follows
  `next_page_uri` on the same host and path, for at most 10 pages. Twilio
  lists newest `date_sent` first, so the listing ends at the first page that
  reaches a message older than the lookback, even if Twilio applies
  `DateSent>=` to the whole date. A larger window is halved by `DateSent<=`
  toward the watermark until it lists completely; more than 2,000 messages
  within two minutes of the watermark fails the poll. While a held message
  keeps the watermark back, the rest of a halved window waits for the next
  poll instead of being relisted at once. Page tokens are never stored, so a
  rejected page restarts the listing on the next poll.
- **Reset.** A cursor that cannot be decoded, or that belongs to another
  account or receiving number, records a new baseline and logs a WARN
  `poll cursor invalid; reseeding` record. Messages in the gap are not
  delivered.
- **Rate limits.** A 429 (Twilio error
  [20429](https://www.twilio.com/docs/api/errors/20429), an account
  concurrency limit) pauses every binding of the connection for `Retry-After`,
  or backs off exponentially without it. A 401 or 403 asks to update the
  connection or grant the API key Messages read access. Other failures back off
  per binding up to 15 minutes. None of them moves the cursor.
- **Delivery.** Delivery is at least once. The Trigger event ID is
  `twilio-message-<SID>`, so a refetch yields the same ID and a Flow target
  keyed by it starts one Flow per message.
- **Poll interval.** The connection's `pollInterval` setting is the delay
  between polls, at least 5 seconds.

`messaging.NewProjectMessageTriggerRunner` opens the connection, loads each
saved binding, and records every message in the binding's project Trigger inbox
before the watermark commits in its project poll cursor, as
[`examples/message-received/main.go`](go/examples/message-received/main.go)
does:

```go
triggerRunner, err := messaging.NewProjectMessageTriggerRunner(project, messagereceived.ConnectionName, messaging.ProjectMessageTriggerRunnerConfig{
	MessageReceivedRoutes: []messaging.ProjectMessageReceivedTriggerRoute{{
		BindingName: messagereceived.MessageReceivedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, messagereceived.AcceptEveryInboundMessage, messagereceived.ResolveFlowID,
			messagereceived.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, messaging.WithLogger(logger))
```

`messaging.NewMessageTriggerRunner` takes targets and cursor stores directly.
The generated `messaging.NewMessageReceivedTrigger` keeps the cursor in memory,
so every start records a new baseline. Delivery status callbacks remain
unimplemented; read delivery status with `getMessage`.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the same module owns its real
Worker, retry, RPC, Timer, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/twilio/messaging/connector.yaml
go run ./cmd/connectorctl generate --check connectors/twilio/messaging/connector.yaml
```

The deterministic provider fakes cover acceptance, conclusive 4xx rejections
(invalid `To`, unverified trial recipient, unsubscribed recipient,
authentication), 429 with `Retry-After`, 503 retry, 500 uncertainty, a timeout after dispatch, a
refused connection, redirects, oversized and malformed responses, and secret
reflection. The example's real Dex integration tests are listed in its
[README](go/examples/text-message-delivery/README.md). The `messageReceived`
fetcher tests cover the baseline, oldest-first delivery without repeats,
outbound and other-number filtering, a held `receiving` MMS, multi-page
listings over 500 messages, date-only `DateSent` filtering, a busy window
halved toward the watermark while a message is held, 429 with `Retry-After`, cursor resets, and a
rejected credential; the
[message-received example](go/examples/message-received/README.md) runs the
Trigger on real Dex.

No live Twilio credentials were used. The following live behavior is
unverified: a real Twilio 201 response and message resource shape, delivery
status progression and error codes from carriers and WhatsApp, 429 and 503
behavior under load, regional endpoints, Restricted API key permissions,
test-credential rejection of `getMessage`, and the Console paths in the setup
guidance. For `messageReceived`, the live Messages list response shape and
`next_page_uri`, whether `DateSent>=` and `DateSent<=` honor the time of day
rather than only the date (the fake covers both; bounds are also applied
locally, and a date-only `DateSent<=` makes halving a busy window ineffective), the `receiving` status of inbound
MMS, `date_sent` timing for inbound messages, and live 429 `Retry-After`
behavior are unverified.

## Python

The `python/` package publishes `dex-connector-twilio-messaging` v0.2.0 with `sendMessage` and `getMessage` Steps and the `messageReceived` Trigger on the Python poll Trigger kit (`dex-connectors-sdk>=0.12.0`). Its cursor documents match Go's byte for byte, so Go and Python replicas share a binding's cursor and inbox. Its [runnable delivery example](python/examples/text_message_delivery) sends one message through a static `twilio-messaging` connection, then reads its status, and its [inbound message example](python/examples/message_received/README.md) starts one Flow per inbound message from the `inbound-messages` binding. The send uses the Dex single-dispatch fence; an uncertain outcome must be reconciled before sending again. Configure the Twilio Account SID, sender, and selected credential method through Dex Web Connections. Live Twilio behavior requires a user-controlled account and has not been verified by local replay.

| Capability | Kind | Languages |
| --- | --- | --- |
| `sendMessage`, `getMessage` | Operations | Go, TypeScript, Python |
| `messageReceived` | Trigger | Go, Python |

## TypeScript package

The TypeScript package `@superdurable/connector-twilio-messaging@0.2.0`
implements `sendMessage` and `getMessage` with the same branch and retry
contract as Go, and the `messageReceived` Trigger on the TypeScript poll
Trigger kit (`@superdurable/connectors-sdk` 0.12.0). Its cursor documents match
Go's byte for byte, so Go and TypeScript replicas share a binding's cursor and
inbox. See [the package README](typescript/README.md), the
[runnable delivery example](typescript/examples/text-message-delivery/README.md),
and the [inbound message example](typescript/examples/message-received/README.md).
Live Twilio authorization, sends, status reads, and Messages listing remain
unverified.

| Capability | Kind | Languages |
| --- | --- | --- |
| `sendMessage`, `getMessage` | Operations | Go, TypeScript, Python |
| `messageReceived` | Trigger | Go, TypeScript |
