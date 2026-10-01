# Webhook Connector

The generic Webhook connector connects a Dex application to any service that
sends or receives webhooks:

- the `requestReceived` Trigger verifies each incoming request, gives it a
  stable event ID, and starts or resumes a Flow through an application target;
- the `sendEvent` Mutation POSTs one JSON event to a configured HTTPS receiver,
  signed with the [Standard Webhooks](https://www.standardwebhooks.com) scheme.

It covers senders that have no dedicated connector, such as form services
(Typeform, Tally, Jotform), GitHub repository webhooks, Shopify admin
webhooks, and any Standard Webhooks sender.

## Architecture

The connection's client keeps one `webhooktrigger.Endpoint` per connection.
`Connection.RequestReceivedWebhookHandler` returns it as an `http.Handler`,
and every `requestReceived` binding built from the same connection is a source
of that endpoint. For each request the endpoint:

1. accepts only `POST` and reads at most `maxBodyBytes`, answering `405` or
   `413` otherwise;
2. resolves the signing secret and verifies the request with the configured
   scheme, answering `400` for a failed check and `503` while the secret is
   missing or unusable;
3. decodes a JSON or form body and derives the event ID, answering `400` for
   another content type or a missing event ID;
4. records the event in the durable inbox of every binding whose
   `matchPointer` accepts it, and answers `200` only after every record is on
   disk; an event that no binding accepts is answered `200` and dropped;
5. delivers recorded events to each binding's target in arrival order.

The endpoint answers `503` while no binding runs, while it replays inboxes
after a restart, when a record fails, and when a binding's queue is full, so
the sender retries. Responses never carry verification or decoding detail.

Delivery is at least once. The sender redelivers anything it did not see
acknowledged, and a crash between the inbox write and delivery replays the
event on the next start. Applications deduplicate with the event ID: the
`sdkgo.NewDexFlowTriggerTarget` start request ID is the event ID, so a Flow ID
derived from it starts one Flow per event.

`NewLocalRequestReceivedEndpointRunner` builds the whole local setup: it loads
the connection and each route's stored binding, wraps each target in a durable
inbox, and combines the endpoint with the bindings' Trigger runners. Its
`RunningSourceCount` supports a readiness check. The checked-in example wires
it like this, from
[`examples/form-submission/main.go`](examples/form-submission/main.go):

```go
func newSubmissionEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *formsubmission.Flow, logger *slog.Logger, connectionOptions []webhook.Option,
) (*webhook.RequestReceivedEndpointRunner, error) {
	bindingLogger := logger.With("connector", webhook.ConnectorID, "connection", formsubmission.ConnectionName,
		"trigger", webhook.RequestReceivedTriggerDefinition.Trigger.TriggerName, "binding", formsubmission.SubmissionTriggerBinding)
	return webhook.NewLocalRequestReceivedEndpointRunner(store, formsubmission.ConnectionName, []webhook.LocalRequestReceivedTriggerRoute{{
		BindingName: formsubmission.SubmissionTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, formsubmission.AcceptSubmission, formsubmission.ResolveFlowID,
			formsubmission.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, append(slices.Clone(connectionOptions), webhook.WithLogger(logger))...)
}
```

Start the runner as soon as the process starts, before the Dex Server is
reachable: the endpoint then records submissions during a Dex outage and
delivers them when Dex returns.

## Security boundary

- There is no unauthenticated mode. Every request is checked with
  HMAC-SHA256, Standard Webhooks, or a shared token, compared in constant time.
- Standard Webhooks requests outside `timestampToleranceSeconds` are rejected,
  which stops replays of captured requests.
- `signing_secret` is a `secretString` credential. It never enters Flow
  input, Attributes, Results, receipts, logs, or HTTP responses. Records carry
  event IDs only.
- Events carry only the headers named in `forwardedHeaders`.
  `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`,
  `webhook-signature`, and the configured `signatureHeader` and `tokenHeader`
  cannot be forwarded; naming one fails connection construction.
- `sendEvent` sends only to the configured HTTPS `deliveryUrl`, never to a URL
  from Flow input, and never follows a redirect.
- The body is application data. It is stored in the binding's inbox file and
  in Flow input, so keep the connection file directory private.

## Sender setup

Configure the sender's webhook to POST to the public HTTPS URL where the
application mounts the handler, then copy its secret into `signing_secret`
and choose the matching configuration:

| Sender | Where the secret is | Configuration |
| --- | --- | --- |
| GitHub | Repository **Settings > Webhooks > Add webhook**: type a secret, such as the output of `openssl rand -hex 32`, and choose **Content type** `application/json` | `verification: hmacSha256`, `signatureHeader: X-Hub-Signature-256`, `signaturePrefix: sha256=`, `eventIdHeader: X-GitHub-Delivery` |
| Shopify admin webhooks | **Settings > Notifications > Webhooks**: the page shows the key every webhook is signed with | `verification: hmacSha256`, `signatureHeader: X-Shopify-Hmac-Sha256`, `signatureEncoding: base64`, `eventIdHeader: X-Shopify-Webhook-Id` |
| Typeform | Form **Connect > Webhooks > Add a webhook**, then **Edit > Secret**: type a secret | `verification: hmacSha256`, `signatureHeader: Typeform-Signature`, `signaturePrefix: sha256=`, `signatureEncoding: base64`, `eventIdPointer: /event_id` |
| A Standard Webhooks sender | The endpoint's signing secret, starting with `whsec_` | `verification: standardWebhooks`; the event ID is always `webhook-id` |
| A sender that cannot sign | A static header value you choose in its custom-header setting | `verification: sharedToken`, `tokenHeader` set to that header |

Senders change their consoles over time; follow the sender's webhook
documentation when a path differs.

### Verification schemes

- `hmacSha256` compares the HMAC-SHA256 of the raw body, keyed with the
  secret's bytes, with the signature in `signatureHeader` after removing
  `signaturePrefix` and decoding `signatureEncoding` (`hex` or `base64`).
- `standardWebhooks` requires `webhook-id`, `webhook-timestamp`, and
  `webhook-signature`. It signs `webhook-id.webhook-timestamp.body` and accepts
  the request when any space-separated `v1,<base64>` entry matches, so a sender
  can rotate keys; other versions, such as `v1a`, are ignored. A `whsec_`
  secret is base64-decoded after the prefix; any other secret is used as its
  bytes.
- `sharedToken` requires `tokenHeader` to equal the secret exactly.

### Event IDs and deduplication

The event ID is the first source that applies:

1. `webhook-id` with `standardWebhooks`;
2. `eventIdHeader`, when it is set and the request carries the header;
3. `eventIdPointer`, an RFC 6901 JSON Pointer into the body, when it is set.
   The value must be a non-empty string or number;
4. otherwise the hex SHA-256 of the body.

An event ID is 1 to 256 printable ASCII characters without spaces; anything
else is answered `400`, so the sender's logs show the misconfiguration.

Prefer a sender delivery ID (sources 1 to 3). It stays the same across every
redelivery of one event, even when the sender adds a delivery timestamp or
attempt counter to the body. The body digest needs no configuration but
deduplicates only byte-identical redeliveries; it also merges two distinct
events whose bodies are identical. A pointer that names a business value,
such as an order ID, merges every event about that value into one Flow.

For a form body, pointers address `{"field": ["value", ...]}`: `/email/0` is
the first `email` value.

## The requestReceived Trigger

Each event's payload is a `WebhookRequestEvent`:

- `ContentType` is `application/json` or
  `application/x-www-form-urlencoded`, without parameters;
- `JSONBody` holds a JSON body unchanged, and `FormBody` holds a decoded form
  body; exactly one is set;
- `Headers` holds the present `forwardedHeaders`, keyed by canonical name,
  with repeated values joined by `, `;
- `ReceivedAt` is when the endpoint read the request; it is also the event's
  `OccurredAt`.

A binding's `RequestReceivedTriggerConfiguration` filters the events it
records. `matchPointer` names a value in the body and `matchValues` lists the
accepted values. A string matches its text, and a number or boolean matches its
literal JSON text. Both are blank to accept every event, and they are set
together. An event a binding filters is still answered `200`. Applications
still enforce their own admission rule with the target's `TriggerFilter`.

The connector declares no Studio units, so a Flow's binding declares no
configuration paths. Dex Web `cli-v1.1.0` therefore saves a binding only as
`{}` and rejects `matchPointer` with `CONNECTOR_TRIGGER_CONFIGURATION_INVALID`.
Add the filter in the `triggerBindings` array of the connection file Dex Web
writes; Dex Web reads it back unchanged. `configuration` may be `{}`:

```json
{
  "connectorId": "webhook",
  "connectionName": "webhook-form",
  "triggerName": "requestReceived",
  "bindingName": "form-submission-received",
  "configuration": {"matchPointer": "/event_type", "matchValues": ["form_response"]}
}
```

## The sendEvent Mutation

`sendEvent` POSTs `SendEventInput.Payload`, one JSON value of at most
`maxBodyBytes`, to `deliveryUrl` with `Content-Type: application/json` and:

- `webhook-id`: `msg_` followed by the Step's 32-hex-digit call ID. Every
  retry and duplicate dispatch of one Step execution sends the same ID, so a
  receiver that deduplicates by `webhook-id` applies the event once;
- `webhook-timestamp`: the attempt's Unix time in seconds;
- `webhook-signature`: `v1,` and the base64 HMAC-SHA256 of
  `webhook-id.webhook-timestamp.body`, keyed as the verification schemes
  describe. Any Standard Webhooks library verifies it with the same `whsec_`
  secret.

| Outcome | Result |
| --- | --- |
| `2xx` | `delivered`, with the status code |
| `408` or `429` | Retry with the same `webhook-id`, after `Retry-After` when sent |
| Connection refused, DNS or TLS failure before the request was written | Retry with the same `webhook-id` |
| A redirect or another `4xx` | `rejected` (optional), with the status code |
| `5xx`, timeout, or a lost connection after the request was written | `uncertain` (optional) |
| Blank `deliveryUrl`, unusable secret, or an invalid payload | `defect` (optional), before any request |

`uncertain` means the receiver may already have applied the event: it can
fail after committing it, or its answer can be lost. The connector does not
retry it, because it cannot know whether this receiver deduplicates by
`webhook-id`. Route `uncertain` to a Step that reconciles with the receiver,
or accept a possible duplicate: sending again from a new Step execution uses
a new `webhook-id`. Retries stop at the Step's retry policy, five attempts
within two minutes, and then fail the Step.

A blank `deliveryUrl` selects `defect` with the guidance to set it in Dex Web
Connectors. Construction fails for a `deliveryUrl` that is not an absolute
HTTPS URL or that holds user information or a fragment.

## Configuration

Every field has a manifest default or a documented blank meaning; Dex Web
Connectors shows the guidance in `connector.yaml`. Configuration is read when
the application starts, so restart it after a change; a replaced
`signing_secret` applies to the next request.

## Hosted mode

The connector does not serve HTTP itself: the application mounts the handler,
so the sender needs a public HTTPS URL for that process. In local development
that is an HTTPS tunnel to the example's listener. How a hosted Dex
application receives a public webhook URL, and whether the platform
terminates TLS and routes it to the right Worker, is an open platform
question this connector does not answer.

## Example

[`examples/form-submission`](examples/form-submission) is a Worker that mounts
the endpoint and starts one `WebhookFormSubmission` Flow per verified
submission. The Flow records the submission and forwards it to `deliveryUrl`
with `sendEvent`.

## Verification

The module pins `sdkgo v0.16.0`, the release that adds `webhooktrigger`.
Until it is published, run the checks in the repository workspace with a
local `sdkgo` replacement in the ignored `go.work`:

```bash
make workspace
go work edit -replace github.com/superdurable/dex-connectors-library/sdkgo@v0.16.0=./sdkgo
cd connectors/superdurable/webhook
go test -race ./...
go vet ./...
```

After the release, run them standalone, as CI does:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The real Dex tests in the example need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/form-submission/... -count=1 -v
```
