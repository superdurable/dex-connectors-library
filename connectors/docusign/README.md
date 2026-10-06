# DocuSign eSignature Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack, through `-tags=integration` tests against `dexcli dev`, with a local stand-in for DocuSign; the example was not started from Dex Web Start Flow, and no live DocuSign account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The DocuSign connector sends envelopes through the
[eSignature REST API v2.1](https://developers.docusign.com/docs/esign-rest-api/reference/)
and resumes Flows from [DocuSign Connect](https://developers.docusign.com/platform/webhooks/connect/).
It is built for the durable envelope pattern: create from a template, wait for
people outside the company for days, resume on Connect, keep the signed
artifact, and void as compensation.

| Kind | Name | What it does |
| --- | --- | --- |
| Mutation | `createEnvelopeFromTemplate` | Creates one envelope from a server template, fills its roles in routing order, and sends it or keeps a draft. |
| Query | `getEnvelope` | Reads an envelope's status, timestamps, void reason, and text custom fields. |
| Query | `listEnvelopeRecipients` | Lists every recipient in routing order with status and sent, delivered, signed, and declined times. |
| Mutation | `voidEnvelope` | Voids a sent or delivered envelope with a reason; an already voided envelope is `voided` again. |
| Query | `downloadCombinedDocument` | Streams the combined PDF through SHA-256 into an application `CombinedDocumentStore`; the Result holds only size, digest, and location. |
| Trigger | `envelopeEventReceived` | Receives HMAC-verified Connect `envelope-completed`, `envelope-declined`, and `envelope-voided` events in the JSON SIM format. |

The runnable [`examples/envelope-signing`](examples/envelope-signing/README.md)
application sends an envelope, waits for Connect or a slow status poll, voids
at a signing deadline, and records the signed PDF's digest.

## Authorization

A connection authorizes one DocuSign user with the confidential
[Authorization Code Grant](https://developers.docusign.com/platform/auth/confidential-authcode-get-token/).
Manifest OAuth endpoints are static, and DocuSign's two environments have
different account servers, so the connector declares two methods:

| Method | Account server | eSignature hosts |
| --- | --- | --- |
| `docusign-oauth` (Production account, default) | `account.docusign.com` | one label below `docusign.net`, such as `na3.docusign.net` |
| `docusign-developer-oauth` (Developer account) | `account-d.docusign.com` | `demo.docusign.net` only |

Both request the scopes `signature`, which every eSignature call needs, and
`extended`, which makes each refresh return a refresh token with a full
lifetime, so an idle connection survives longer than 30 days as long as it
refreshes. `impersonation` is not requested: the connector has no JWT grant.
Access tokens last eight hours. Before a call the connector refreshes a token
that is missing, has no recorded expiry, or expires within five minutes,
through `sdkgo/oauthtoken` with the client as HTTP Basic credentials, as
DocuSign documents. `invalid_request`, `invalid_client`, `invalid_grant`, and
`unauthorized_client` below HTTP 500 mark the connection
`reauthorization_required`. After a `401` the connector refreshes once and
repeats the request once.

Dex Web's code exchange sends `client_id` and `client_secret` in the form
body. DocuSign's guide shows HTTP Basic, but both
`https://account.docusign.com/.well-known/openid-configuration` and the
developer one list `client_secret_post` and `client_secret_basic`, so the
form body is accepted. PKCE is off: DocuSign makes it optional for a
confidential client and spells its method `s256`, and the client secret
already authenticates every exchange.

### The account and its regional host

The API host is not fixed: every DocuSign account lives on a regional host.
On the first call for an access token the connector reads
`GET /oauth/userinfo` from the method's account server, selects the account
named by the connection's `accountId`, or the user's `is_default` account when
it is blank, and accepts its `base_uri` only when it is `https://` plus a host
of that environment with no port, path, query, or user information. Requests
then go to `{base_uri}/restapi/v2.1/accounts/{accountId}`. The answer is
cached for one hour per connection and access token, inside DocuSign's
documented limit of 25,000 userinfo requests per user per hour. A host outside
the environment selects `invalidResponse` and the token is never sent there;
`createEnvelopeFromTemplate` has no `invalidResponse` branch and selects
`providerRejected` instead. An `accountId` the user does not belong to selects
`defect`. On a later create attempt after an earlier one sent the request,
any of these selects `uncertain`, as described below.

## Operations

Every operation maps DocuSign's `errorCode`, never its message text, onto
branches and failure kinds, and puts the code in the Failure message and the
Receipt's `errorCode` metadata. `HOURLY_*` and `BURST_*` limit codes and `429`
retry after `X-RateLimit-Reset`, `Retry-After`, the 30-second burst window, or
the top of the hour; `5xx` and `408` retry. Queries and `voidEnvelope` use
async durability with a 65-minute retry window, so a delay until the next
hourly reset fits. `ENVELOPE_DOES_NOT_EXIST` or `404` selects `notFound`,
authentication and permission codes select `providerRejected`, and
`ENVELOPE_ALLOWANCE_EXCEEDED` is `QUOTA_EXHAUSTED`.

### createEnvelopeFromTemplate and duplicate dispatch

DocuSign has no idempotency key, and a duplicate envelope is a duplicate
contract. The operation therefore:

1. runs with **sync** durability, so Dex never re-dispatches a slow create;
2. adds a hidden text custom field, `dexIdempotencyKey`, holding the Step's
   idempotency key, which is stable across retries of one Step execution;
3. records a heartbeat checkpoint with the dispatch time before the `POST`;
4. after a lost connection, a `3xx`, `408`, or `5xx`, an unreadable `201`, or
   on any later attempt that finds the checkpoint, lists
   `GET /envelopes?from_date=<dispatch - 1h>&custom_field=dexIdempotencyKey=<key>&include=custom_fields`
   and checks the field itself. Exactly one match selects `created` with
   `wasRecovered: true`; none selects `uncertain`, because DocuSign's listing
   may not show a just-created envelope yet. A read-back that DocuSign rate
   limits on a later attempt is retried, keeping the checkpoint. A later
   attempt that finds the checkpoint but cannot obtain credentials or resolve
   the account also selects `uncertain`, never `providerRejected` or `defect`;
5. on a later attempt that finds no checkpoint, lists the same way from an
   hour before the first attempt before it sends: one match is adopted as
   above, several select `uncertain`, and none lets the attempt send. A
   read-back that is lost, rate limited, or answered `5xx` is retried before
   anything is sent.

The create request itself is retried only after a refused rate limit or when
it provably never left the Worker. Dex accepts the checkpoint when the Worker writes it to its stream,
before the request leaves; a Worker lost in that instant, before Dex stored
the checkpoint, leaves the next attempt only the read-back of step 5, which
misses an envelope DocuSign does not list yet, so a second envelope is still
possible in that window. A crash after the checkpoint but before the request
left reports `uncertain` for a request DocuSign never received.

Keep the Step sync: an async local attempt's heartbeat is not visible to its
fallback attempt, so an application override to async durability could send a
slow create twice. `templateRoles` keep their `routingOrder`, which decides
who is asked to sign first; zero keeps the template's order.

A role name filled twice, a reserved custom field name, a subject over
DocuSign's documented 100 characters, and custom fields beyond the
connector's bounds (10 fields, 50 character names, 100 character values)
select `defect` without a request.

### voidEnvelope

`PUT /envelopes/{id}` with `status: voided` and the reason. When DocuSign
answers `ENVELOPE_CANNOT_VOID_INVALID_STATE`, the connector reads the envelope:
`voided` selects `voided` with `wasAlreadyVoided`, so a repeated Step and a
duplicate dispatch are safe, and `completed`, `declined`, or a `created` draft
select `notVoidable` with the status. A real-Dex test lets the async Step be
dispatched twice against a nine-second provider and still ends `voided`.

### downloadCombinedDocument and Dex payloads

Signed PDFs are binary and can be megabytes, and provider payloads never enter
Results or other durable state, so the PDF stays out of Dex. The operation streams
`GET /envelopes/{id}/documents/combined?certificate=<bool>` through SHA-256
and, when the client was built with `WithCombinedDocumentStore`, into the
application's store, and returns `byteCount`, `sha256`, and the store's
`storedLocation`. It requires `application/pdf` and the `%PDF-` signature,
stops at `maxDocumentBytes` with `tooLarge`, records a nil heartbeat every five
seconds within its 30-second heartbeat timeout, and retries an interrupted
download. A store must replace its object atomically, because Dex can repeat
the download; the example's `directoryDocumentStore` writes a temporary file
and renames it.

## Trigger

The `envelopeEventReceived` Trigger serves one `webhooktrigger.Endpoint` per
connection. `Connection.EnvelopeEventReceivedWebhookHandler` returns it, and
`NewProjectEnvelopeEventReceivedEndpointRunner` wraps every binding in a
durable project inbox. For each delivery the endpoint:

1. accepts only `POST` up to `connectMaxBodyBytes`, answering `405` or `413`;
2. verifies the body against every `X-DocuSign-Signature-<n>` header, one per
   HMAC key of the account, as the base64
   [HMAC-SHA256](https://developers.docusign.com/platform/webhooks/connect/validate/)
   of the whole raw body with `connect_hmac_key`, removing any `"` from the key
   as DocuSign instructs, in constant time. One match is enough. A failure
   answers `400`; a connection without a key answers `503`, so Connect
   retries. An access token that expired while idle is refreshed first, so
   the key stays readable;
3. decodes the [JSON SIM](https://developers.docusign.com/platform/webhooks/connect/json-sim-event-model/)
   message: `envelope-completed`, `envelope-declined`, and `envelope-voided`
   become events, any other event such as `recipient-completed` is
   acknowledged with `200`, and a legacy XML or aggregate body answers `400`;
4. records the event for every binding whose `events` accept it, and answers
   `200` only after every record is in its inbox.

Recorded events survive a Worker restart: the runner replays a binding's
pending inbox events before new ones, and otherwise delivers them in the order
the endpoint recorded them. An envelope reaches only one of the three final
outcomes, so ordering matters only across envelopes, and each envelope resumes
its own Flow.

The event ID is `<envelope id>:<event>`, such as
`93be49ab-0000-0000-0000-f752070d71ec:envelope-completed`. An envelope reaches
each of these outcomes at most once, so a Connect retry, or a second Connect
configuration delivering the same outcome, keeps one ID. The event carries the
account, the generation time, and, when the configuration includes
**Custom Fields**, the envelope's text custom fields and status. An
application correlates an event to its Flow through a custom field it set at
creation, as the example's `dexSigningRequestId` does. JSON SIM events carry
no timestamp signature, so the endpoint cannot reject a replayed capture;
recording is idempotent per event ID, and the example's RPC deduplicates it.

The example resumes the waiting Flow through a typed RPC, from
[`examples/envelope-signing/main.go`](examples/envelope-signing/main.go):

```go
// newEnvelopeEventTarget delivers each Connect outcome to the waiting Flow's ReceiveEnvelopeEvent RPC.
func newEnvelopeEventTarget(client *dex.Client, flow *envelopesigning.EnvelopeSigningFlow, logger *slog.Logger) sdkgo.TriggerTarget[docusign.EnvelopeEvent] {
	bindingLogger := logger.With("connector", docusign.ConnectorID, "connection", envelopesigning.ConnectionName,
		"trigger", docusign.EnvelopeEventReceivedTriggerDefinition.Trigger.TriggerName, "binding", envelopesigning.EnvelopeOutcomeTriggerBinding)
	return sdkgo.NewDexRPCTriggerTarget(client, flow.ReceiveEnvelopeEvent, envelopesigning.AcceptEnvelopeEvent,
		envelopesigning.ResolveFlowID, envelopesigning.MapToEnvelopeEventInput, sdkgo.WithTriggerLogger(bindingLogger))
}
```

The application loads its project configuration with
`projectconfig.LoadFromEnvironment`, which reads the `DEX_PROJECT_*`
environment described in [project configuration](../../sdkgo/projectconfig/README.md),
and opens the connection with `docusign.NewProjectConnection`. The Trigger
runner logs consumed undeliverable events to `slog.Default()`, which the
example sets to its logger.

### Connect configuration

An account administrator creates the configuration in DocuSign
**Admin > Integrations > Connect > Add Configuration > Custom**:

- **URL to Publish**: the application's public HTTPS endpoint, such as
  `https://<host>/docusign/connect`;
- **Data Format**: JSON, **Event Message Delivery Mode**: Send Individual
  Messages (SIM);
- **Trigger Events**: Envelope Signed/Completed, Envelope Declined, and
  Envelope Voided;
- **Include Data**: Custom Fields, so events carry the correlation field;
  never Documents, which would carry the PDFs;
- **Integration and Security Settings**: Include HMAC Signature and Require
  Acknowledgement, so DocuSign retries any delivery not answered `200`.

The HMAC key comes from **Connect > Connect Keys > Add Secret Key** and is shown
in full once. To rotate it, add a new key, enter it in `connect_hmac_key`, then
choose **Remove** next to the old key. DocuSign signs with every key, and the
endpoint accepts a match on any signature header, so neither the overlap nor
the renumbering of headers after a removal refuses a delivery.
Envelope-level Connect (`eventNotification` on the create request) and
creating configurations through the API are not part of this release.

## Configuration UI

This release ships no Studio bundle. A template picker would read
`{base_uri}/restapi/v2.1/accounts/{accountId}/templates`, but Studio setup
commands need a fixed host and DocuSign's API host differs per account, so
template IDs are Flow input. An account picker is possible and is a known gap:
the accounts come from `GET /oauth/userinfo` on the fixed account server of
each environment, so one read-only command per environment and a picker unit
on `accountId` could list them. It is deferred because it needs this
connector's first Studio `ui/` bundle, so `accountId` stays a documented
connection field whose blank value uses the user's default account.

The authorization form shows the integration key, its secret key, and the
optional Connect HMAC key; tokens are produced by authorization. With no units,
Dex Web saves an `envelopeEventReceived` binding only as `{}`, which records
all three outcomes; an `events` filter belongs in the binding's record in the
project configuration's `triggerBindings` array.

## Security boundary

- The client secret, tokens, and the HMAC key are `secretString` credentials.
  They never enter Flow input, Attributes, Results, receipts, logs, or HTTP
  responses, and Results never hold DocuSign message text, access codes, or
  document bytes.
- The access token goes only to the method's account server and to an
  eSignature host validated for that environment; requests never follow a
  redirect, and JSON bodies are read up to `maxResponseBytes`.

## Unverified live behavior

No DocuSign account was used. These rely on documentation fetched on
2026-10-04:

- whether DocuSign accepts Dex Web's form-body client authentication on the
  code exchange (its discovery documents list `client_secret_post`), and
  whether the token response's `scope`, when present, lists `signature` and
  `extended` exactly as Dex Web requires;
- whether `GET /envelopes` filtered by `custom_field` lists a just-created
  envelope at once; when it does not, an ambiguous create selects `uncertain`,
  and a retry whose checkpoint Dex never stored can send a second envelope;
- the HTTP status DocuSign pairs with each `errorCode`; the connector keys on
  the code, and the status only as a fallback;
- whether the `X-RateLimit-Reset` header is a Unix time on every limit answer;
- Connect retry timing and whether a retried JSON SIM message is byte-identical;
- the Developer Console and Admin page paths in the setup guidance.

## Verification

```bash
cd connectors/docusign
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

The integration run needs `dexcli dev` and takes about half a minute: the
create and void duplicate-dispatch tests wait for a nine-second provider, and
the example's tests send an envelope, resume it from a signed Connect event
and its duplicates, survive a Worker replacement during the wait, resume from
the fallback status poll, and void at the signing deadline.
