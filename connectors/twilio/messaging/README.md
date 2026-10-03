# Twilio Messaging Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Twilio; no live Twilio account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module sends SMS, MMS, and WhatsApp messages through Twilio Programmable
Messaging and reads their delivery status:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `sendMessage` | Mutation | `accepted` | `providerRejected`, `uncertain`, `defect` |
| `getMessage` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Only a safe message view crosses the connector boundary: the message SID,
account and Messaging Service SIDs, sender, recipient, Twilio's own status
value, error code, segment and media counts, price, and timestamps. Results,
receipts, failures, and logs never contain the message body, media URLs,
Twilio's error message text, the Auth Token, or the API key secret.

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

### No sender picker yet

The manifest declares no Studio bundle, so the default sender is a guided text
field rather than a picker built from the account's IncomingPhoneNumbers and
Messaging Services. Twilio's REST API accepts only HTTP Basic authentication,
while Studio setup commands support only a `bearer` credential or a raw secret
in a named header, and `Authorization` is a reserved header. A picker therefore
cannot authenticate until the manifest schema and the Dex Web broker gain a
Basic credential scheme that pairs a non-secret username with a secret field.

Dex Web `cli-v1.1.0` also omits auth-method `configuration.fields` from the
setup form, so the API key SID is a non-secret credential field of the
`api-key` method instead of method configuration.

## Local configuration

Dex Web writes this file for the connection name the application uses:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "twilio-messaging",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging",
    "moduleVersion": "v0.1.0",
    "provider": "twilio",
    "connectionName": "twilio-messaging",
    "configuration": {"accountSid": "AC...", "defaultSender": "+14155550100"},
    "credentials": {"auth_method": "api-key", "api_key_sid": "SK...", "api_key_secret": "..."}
  }]
}
```

The `auth-token` method stores `{"auth_method": "auth-token", "auth_token":
"..."}` instead. Load the file with `localconfig.LoadFromEnvironment` and
`messaging.NewLocalConnection(store, "twilio-messaging")`.

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
`auth_method` with `auth_token`, or with `api_key_sid` and `api_key_secret`;
when `auth_method` is absent, the one secret present selects the method:

```go
provider, err := hostedconfig.NewCredentialProviderFromEnvironment(
    messaging.ConnectorID,
    "twilio-messaging",
    messaging.DecodeResolvedCredentialsJSON,
)
if err != nil {
    return err
}
client, err := messaging.New(messaging.Config{AccountSID: accountSID}, provider)
```

## Operations

### sendMessage

`SendMessageInput` names the recipient `to` (E.164, or `whatsapp:` plus
E.164), a `body` of at most 1600 characters, up to 10 public HTTPS
`mediaUrls`, and an optional `sender` that overrides the default sender. A
WhatsApp recipient needs a WhatsApp sender or a Messaging Service. Invalid
input selects `defect` without calling Twilio.

Twilio's Messages API has no idempotency key: a repeated create request sends a
second text. The connector therefore retries only when Twilio cannot have
created a message:

| Outcome | Result |
| --- | --- |
| 2xx with a valid message | `accepted`, with the SID and status, usually `queued` or `accepted` |
| 400, 401, 403, 404, 409, or another 4xx except 408 and 429 | `providerRejected`, with the Twilio error code in `Value.ErrorCode` and the Receipt's `twilioErrorCode` |
| 429 | Retry, after `Retry-After` when Twilio sends it |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 408, 3xx, or 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, for another account, or reflects the credential | `uncertain` |

The connector bounds every request by 20 seconds, below the 30-second Execute
timeout, so it observes a hung send and selects `uncertain` before Dex would
retry the Step. It never sends an `Idempotency-Key` header, because Go's HTTP
transport treats a request with that header as safe to replay. A transport
that reports no connection trace events counts as dispatched.

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

`sendMessage` uses sync Execute durability even though a send is usually fast.
With async durability, Dex runs the Step in a local phase of about seven
seconds and then starts a fallback attempt, so a slow request is sent a second
time while the first is still in flight. A real Dex run with a Twilio fake that
held the response for nine seconds recorded two create requests under async
durability and one under sync; `TestSlowDispatchIsSentOnceWithRealDex` guards
this. The same reason makes `generateText` sync. `getMessage` is a read and
keeps the async default. Do not override `sendMessage` to async.

The connector never retries a dispatched request, but Dex runs a Step at least
once. If the Worker is lost after Twilio accepts a send and before Dex commits
the Step result, Dex runs the Step again and Twilio sends a second text. No
connector can close this window without a provider idempotency key.

An application handles `uncertain` without resending automatically.
[`examples/text-message-delivery`](examples/text-message-delivery) shows the
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

The send Step wiring in the example:

```go
dex.DefineStep(messaging.NewSendMessageStep(messaging.SendMessageStepConfig[Input]{
	StepType: sendTextMessageStepType, ConnectionName: ConnectionName,
	Annotations: sdkgo.StepAnnotations{
		GroupID: "twilio", GroupLabel: "Twilio",
		Explanation: "Send the text once; an unknown outcome is reconciled, never resent automatically.",
	},
	Connection: flow.connection, MapToOperationInput: flow.MapToSendMessageInput,
	Accepted:         sdkgo.GoTo(recordAcceptedTextMessage{}),
	ProviderRejected: sdkgo.GoTo(recordRejectedTextMessage{}),
	Uncertain:        sdkgo.GoTo(recordUncertainTextMessage{}),
})),
```

## Not in this release

Inbound SMS and delivery status callbacks need a Trigger, which waits for the
shared webhook source design. Until then, read delivery status with
`getMessage`.

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
authentication), 429 with `Retry-After`, 5xx, a timeout after dispatch, a
refused connection, redirects, oversized and malformed responses, and secret
reflection. The example's real Dex integration tests are listed in its
[README](examples/text-message-delivery/README.md).

No live Twilio credentials were used. The following live behavior is
unverified: a real Twilio 201 response and message resource shape, delivery
status progression and error codes from carriers and WhatsApp, 429 and 5xx
behavior under load, regional endpoints, Restricted API key permissions,
test-credential rejection of `getMessage`, and the Console paths in the setup
guidance.
