# Typeform Connector

> **Verification status: partial live.** Only a dummy-token request reached Typeform, which returned 403; everything else ran on a real Dex stack against a local stand-in. No live Typeform account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Typeform connector connects a Dex application to the Typeform
[Create, Responses, and Webhooks APIs](https://www.typeform.com/developers/get-started/):

| Name | Kind | What it does | Happy branch |
| --- | --- | --- | --- |
| `listForms` | Query | One page, 1 to 200, of the account's forms, optionally by search text or workspace | `listed` |
| `getForm` | Query | One form's fields with their refs, types, titles, and choices, and its hidden field names | `found` |
| `listResponses` | Query | One page, 1 to 1000, of a form's completed responses, bounded by `since`/`until` and paged by `before`/`after` token | `listed` |
| `upsertWebhook` | Mutation | Creates or replaces the form's webhook under one tag, with the URL, enabled state, and `webhook_secret` | `upserted` |
| `responseSubmitted` | Trigger | A signed `form_response` webhook, decoded into typed answers keyed by field | |

Every other branch is optional: `notFound` (except `listForms`),
`providerRejected`, `invalidResponse`, and `defect`. Every time in a Result is
UTC.

## Authorization

A connection uses a Typeform **personal access token**, created at
[Account > Personal tokens](https://admin.typeform.com/user/tokens) with the
scopes `forms:read`, `responses:read`, and `webhooks:write`. Typeform shows the
`tfp_` token once and does not expire it; a `401` or `403` selects
`providerRejected` with `AUTHENTICATION` or `AUTHORIZATION`.

`webhook_secret` is optional and needed only by the Trigger and
`upsertWebhook`: it is a secret you generate, such as `openssl rand -hex 32`,
that `upsertWebhook` registers with Typeform and the endpoint verifies every
delivery with.

The manifest declares the token as the single method of `auth.methods`, so a
Typeform OAuth method can be added later without changing saved connections.
OAuth is not offered yet: Typeform's
[token response](https://www.typeform.com/developers/get-started/applications/)
lists `token_type`, `access_token`, `expires_in`, and `refresh_token` but no
`scope`, and Dex Web `cli-v1.2.0` rejects an authorization whose token response
does not list every declared scope (`CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`).

## Data centers

`dataCenter` picks the API host of every operation: `us` is
`api.typeform.com`, `eu` is `api.eu.typeform.com`, and `newEu` is
`api.typeform.eu`. Typeform's
[EU Responses Data Center](https://www.typeform.com/developers/get-started/responses-data-center/)
answers a Responses API request in the wrong region with no responses instead
of an error, and webhooks must be managed in the account's region, so an
Enterprise account in the EU sets it explicitly. Tokens for `newEu` are valid
only on `api.typeform.eu`.

## Operations

### Reads

`listForms` pages by page number and returns `nextPage`, zero on the last page.
`getForm` lists every question in form order; a question inside a `group`
follows its group with `groupFieldId` set. `listResponses` lists completed
responses newest first, sends `since` and `until` in UTC to the second as
Typeform documents, and returns `nextBeforeToken`, the oldest response's token,
while older responses remain. Typeform may omit a response submitted in roughly
the last 30 minutes; the Trigger receives those. `listResponses` and the
Trigger return the same `FormResponse`, so a Flow can backfill and receive alike.

### Answers

Each `FormAnswer` carries the question's `fieldId`, `fieldRef`, `fieldType`,
and, from a webhook's form definition, `fieldTitle`, plus the answer `type` and
exactly one typed value: `text`, `email`, `url`, `fileUrl`, `date`,
`phoneNumber`, `number`, `boolean`, `choice`, `choices`, `payment`,
`signature`, or `multiFormat`. Both documented `choices` shapes, an object of
parallel lists and the checkbox's array, decode to `FormAnswerChoices`. An
answer type the connector does not know keeps its field and `type` with no
value. `FormResponse.AnswerByFieldRef` and `AnswerByFieldID` find one answer;
Typeform documents the refs it generates for editor-built forms as
non-persistent, so match those by field ID. `date` keeps Typeform's text,
`YYYY-MM-DD` in webhooks and an RFC 3339 midnight in the Responses API.
Payment card digits and the cardholder name are not copied.

### upsertWebhook and duplicate dispatch

`upsertWebhook` sends `PUT /forms/{form_id}/webhooks/{tag}` with the URL,
`enabled`, `event_types: {"form_response": true}`, and `webhook_secret`, and
returns the stored webhook without its secret. Typeform has no idempotency
header, but every attempt sends the same body to the same tag, so the
Mutation uses async durability: a lost answer, `408`, `429`, or `5xx` returns
Retry, and a Dex re-dispatch repeats the same `PUT`. The real-Dex test in
`mutation_dispatch_integration_test.go`, whose fake Typeform stores the webhook
at once and answers after nine seconds, observed two `PUT`s and one webhook.
`isDisabled` stores the webhook without delivering to it. A blank
`webhook_secret` selects `defect` before any request, and Typeform's
`webhook_url_https_required` selects `providerRejected` with `VALIDATION`.

## Trigger

The `responseSubmitted` Trigger serves one `webhooktrigger.Endpoint` per
connection. `Connection.ResponseSubmittedWebhookHandler` returns it, and
`NewProjectResponseSubmittedEndpointRunner` opens the connection and its
bindings from the project configuration that `projectconfig.LoadFromEnvironment`
loads and wraps every binding in a durable project inbox. For each delivery
the endpoint:

1. accepts only `POST` up to `webhookMaxBodyBytes`, answering `405` or `413`;
2. verifies `Typeform-Signature: sha256=<base64>` as the
   [HMAC-SHA256](https://www.typeform.com/developers/webhooks/secure-your-webhooks/)
   of the raw body keyed with `webhook_secret`, in constant time. A failure
   answers `400`; a connection without a secret answers `503`;
3. decodes `form_response`, acknowledging `form_response_partial` and any other
   event type with `200`;
4. records the submission for every binding whose `formId` accepts it, and
   answers `200` only after every record is stored.

The event ID is the form ID and the response token, such as
`lT4Z3j:a3a12ec67a1365927098a606107fac15`. Typeform documents the token as the
response ID, so every redelivery of one submission keeps its ID, and so does a
copy sent by a second webhook of the same form. Typeform's own `event_id` is
kept as `webhookEventId`. Typeform signs no timestamp, so a captured delivery
can be replayed; the stable event ID makes the replay a duplicate Flow start.

Typeform retries a `503` every two to three minutes for ten hours and most
other failures, including `400`, on a back-off of up to four hours; it disables
a webhook that answers `404` or `410`.

The checked-in example wires the endpoint like this, from
[`examples/response-recorder/main.go`](examples/response-recorder/main.go):

```go
func newSubmissionEndpointRunner(
	project *projectconfig.LoadedProject, client *dex.Client, flow *responserecorder.Flow, logger *slog.Logger, connectionOptions []typeform.Option,
) (*typeform.ResponseSubmittedEndpointRunner, error) {
	return typeform.NewProjectResponseSubmittedEndpointRunner(project, responserecorder.ConnectionName, []typeform.ProjectResponseSubmittedTriggerRoute{{
		BindingName: responserecorder.ResponseSubmittedTriggerBinding, Target: newSubmissionTarget(client, flow, logger),
	}}, append(slices.Clone(connectionOptions), typeform.WithLogger(logger))...)
}

// newSubmissionTarget starts one Flow per submission, with the Trigger event ID as request ID.
func newSubmissionTarget(client *dex.Client, flow *responserecorder.Flow, logger *slog.Logger) sdkgo.TriggerTarget[typeform.FormResponseEvent] {
	bindingLogger := logger.With("connector", typeform.ConnectorID, "connection", responserecorder.ConnectionName,
		"trigger", typeform.ResponseSubmittedTriggerDefinition.Trigger.TriggerName, "binding", responserecorder.ResponseSubmittedTriggerBinding)
	return sdkgo.NewDexFlowTriggerTarget(client, flow, responserecorder.AcceptSubmission, responserecorder.ResolveFlowID,
		responserecorder.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger))
}
```

Start the runner as soon as the process starts, before the Dex Server is
reachable: the endpoint then records submissions during a Dex outage and
delivers them when Dex returns.

## Configuration UI

The `formPicker` Studio unit reads `GET https://api.typeform.com/forms`, a
fixed-host bearer command whose response is a JSON object, and stores the
chosen `formId`. When that host refuses the token it repeats the read on
`api.typeform.eu` for a new EU data center token; `api.eu.typeform.com`
accounts list their forms on `api.typeform.com`. A manual form ID field covers
a form the list cannot load. The connection surface shows status only; the
token and secret stay in Dex Web's host form and never reach the frame.

## Security boundary

- The token and webhook secret are `secretString` credentials. They never
  enter Flow input, Attributes, Results, receipts, logs, or HTTP responses;
  `upsertWebhook` drops the secret Typeform echoes back.
- The connector sends the token only to the `dataCenter` host, never follows a
  redirect, and reads at most `maxResponseBytes`.
- Failure messages are written by the connector. The only Typeform text it
  reads is the documented error `code` `webhook_url_https_required`.
- Answers are application data: they are stored in the binding's durable
  inbox in the private project storage and in Flow input.

## Unverified live behavior

These rely on the documentation fetched on 2026-09-30, not on a live account:

- whether `event_id` stays the same across Typeform's redeliveries; the
  connector does not depend on it;
- whether Typeform answers a webhook `PUT` with `200` for both a new and an
  updated tag, whether it echoes `form_id`, `tag`, and `enabled` exactly, and
  whether `event_types` replaces or merges an existing webhook's events;
- whether a `newEu` personal access token is created at a different admin
  host than `admin.typeform.com`;
- the `phone_number` answer type, which the webhook payload page does not
  list, and the shape of `nps`, `ranking`, and `matrix` answers;
- Typeform's `429` body and whether it sends `Retry-After`.

## Verification

```bash
cd connectors/typeform
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration run needs `dexcli dev` and takes about twenty seconds, because
the duplicate-dispatch test waits for a nine-second provider.
