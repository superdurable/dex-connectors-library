# Google Forms Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Google Forms; no live Google account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Google Forms Connector reads a form's questions and its responses through
the [Google Forms API](https://developers.google.com/workspace/forms/api/reference/rest).
It is read-only and exposes operation-specific Dex Step factories:

| Name | Kind | What it does | Happy branch |
| --- | --- | --- | --- |
| `getForm` | Query | One form's items in form order, with each question's ID, title, type, and choices | `found` |
| `listResponses` | Query | One page, 1 to 1000, of a form's responses, optionally only those submitted after a time, continued with Google's page token | `listed` |
| `getResponse` | Query | One response by ID, with every answer keyed by question ID | `found` |

Every other branch is optional: `notFound`, `providerRejected`,
`invalidResponse`, and `defect`. Every time in a Result is UTC.

The connector does not create, edit, publish, or delete forms or responses,
and this release has no Triggers; see [Why there is no Trigger](#why-there-is-no-trigger).

## Scopes

Both authorization methods request exactly these canonical scopes:

| Scope | Why |
| --- | --- |
| `https://www.googleapis.com/auth/forms.body.readonly` | `getForm` reads the form's questions. |
| `https://www.googleapis.com/auth/forms.responses.readonly` | `listResponses` and `getResponse` read responses. |
| `https://www.googleapis.com/auth/drive.metadata.readonly` | The Studio form picker lists form files through Google Drive. |

These are the narrowest scopes that cover the operations. Google's reference
pages accept `forms.body.readonly` for `forms.get` and
`forms.responses.readonly` for `forms.responses.list` and `forms.responses.get`;
`drive.readonly` would read form bodies but not responses, and `drive` or
`drive.file` would also allow writes. The Forms API has no way to list forms,
so the picker uses Drive `files.list`. `drive.file` lists only files this OAuth
client created or opened, so it would show an empty picker, and
`drive.metadata.readonly` is the narrowest Drive scope that lists the forms an
account already has. It reads Drive metadata only, never file content.

Google's Drive guide classifies `drive.metadata.readonly` as a restricted
scope: an External app stays in Testing with listed test users, uses an
Internal Workspace audience, or completes Google's restricted-scope verification
before it is published. The Forms reference pages fetched for this release do
not classify the two Forms scopes.

Google reports alias grants under their canonical URIs and Dex Web compares
granted scopes literally, so the manifest names each scope by its full URI. A
refresh or delegated token that reports fewer scopes requires reauthorization,
and Google lets a user uncheck individual scopes on the consent screen, so the
authorization guide asks the user to keep every scope checked.

## Authorization

- `google-oauth` is recommended for personal Google accounts and ordinary
  Workspace users. Dex requests offline access and refreshes the access token
  before it expires.
- `workspace-domain-delegation` is an administrator-only option. It signs an
  RS256 service-account assertion for `delegated_user` with the same three
  scopes and mints a delegated access token.

Authorize an account, or delegate to a user, that can edit the forms a Flow
reads. Google Forms shows responses to a form's editors, and Google's watches
guide stops notifications when a user loses edit access; the API reference does
not state the permission `responses.list` checks.

`CredentialRefreshDriver` delegates both token exchanges to
`sdkgo/oauthtoken`: the refresh-token grant with `ClientSecretPost`, and the
JWT-bearer grant with `SignJWTBearerAssertion`. Google access tokens always
expire, so a token without a recorded expiry is refreshed
(`RefreshWhenExpiryMissing`). `invalid_grant`, `invalid_client`,
`unauthorized_client`, missing refresh material, an invalid service-account
key or delegated user, and a returned scope list without all three scopes
require reauthorization. A Google 5xx is always retryable. If Google omits a new
refresh token, the prior value is kept. A 401 forces one coordinated refresh and
one resend, never a refresh loop.

The driver never owns persistence. Local development reloads and atomically
replaces the private `0600` connection file. Hosted apps receive only an
operation-scoped access token and the selected method from the Superverse
broker: `DecodeResolvedCredentialsJSON` rejects refresh tokens, client
secrets, and service-account keys, which stay in the encrypted credential
store. `DecodeCredentialsJSON` and `EncodeCredentialsJSON` are the broker's
trusted decode and persistence hooks.

For local Dex Web setup, name the factory connection and load the same name at
application startup, as [`examples/response-recorder/main.go`](examples/response-recorder/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := forms.NewLocalConnection(store, responserecorder.ConnectionName)
```

## Reading a form

`getForm` returns the title, Drive document title, description, responder
link, linked Sheets ID, quiz flag, email collection setting, publish state, and
every item in form order. Each `FormItem` has a `kind`: `question`,
`questionGroup`, `pageBreak`, `text`, `image`, `video`, or `unknown` for a kind
Google adds later. Question and grid items carry their `questions`:

- A question item has one question whose `title` is the item title.
- A grid (`questionGroup`) has one question per row, whose `title` is the row
  title and whose `choices` are the grid's columns; the item title is the
  shared prompt.

`QuestionType` is `radio`, `checkbox`, `dropDown`, `shortText`, `paragraph`,
`scale`, `date`, `time`, `fileUpload`, `rating`, `radioGrid`, `checkboxGrid`,
or `unknown`. A question of a kind this release does not know keeps its ID and
title, so its answers can still be labeled. `Form.Questions` flattens every
question in form order and `Form.QuestionByID` finds one.

The form ID is the ID in the edit link
`https://docs.google.com/forms/d/FORM_ID/edit` and the form's Drive file ID.
The responder link `.../forms/d/e/ID/viewform` carries a different ID that the
Forms API does not accept.

## Listing responses

`listResponses` sends exactly one of Google's two submission-time filters, in
UTC with nanoseconds:

- `submittedAfter` sends `timestamp > N`, responses submitted after but not at N;
- `submittedAtOrAfter` sends `timestamp >= N`.

Setting both, a page size outside 1 to 1000, or a malformed form ID or page
token selects `defect` without a request. Google returns up to 5000 responses
when no page size is sent, so the connector always sends one: `pageSize`, or
the connection's `responsePageSize`. A page token works only with the same
form and filter as the request that returned it.

Each `FormResponse` carries the response ID, `createdAt` (first submission),
`lastSubmittedAt` (latest submission; later after an edit, unchanged by
grading), the respondent email when the form collects one, the quiz total
score, and the answers sorted by question ID. Each `FormAnswer` holds the
answer's text `values` (one per selected checkbox option), uploaded `files`
with their Drive IDs, and a quiz `grade`. A skipped question has no answer.

`listed` includes an empty page. Google may return a short or empty page while
`nextPageToken` is set, and it documents no order for the listing, so the
connector keeps Google's order and an application must read every page before
it treats a submission time as complete.

## Why there is no Trigger

Google Forms delivers change notifications ("watches") only to a Cloud Pub/Sub
topic, which the application must create and grant to
`forms-notifications@system.gserviceaccount.com`. A notification carries only
the form ID and event type, and a watch expires after a week unless renewed.
That is not an HTTPS webhook, so `sdkgo/webhooktrigger` does not fit.

A poll `responseSubmitted` Trigger, modeled on Gmail's poller, would need a
durable cursor, and `sdkgo` cannot persist one cleanly today. A
`TriggerSource` receives only a context and a target. The generated source
hook receives the connection reference and binding configuration, but no
store and no binding name. `localconfig` persists pending events but no source
state, and `hostedconfig` has no Trigger state. Gmail keeps its position in
memory and rescans the newest inbox page after a restart. That works because
Gmail lists newest first. The Forms listing has no documented order, so a
bounded rescan cannot find responses submitted while the process was down.

The cursor a Forms poller needs, per binding, is:

1. state: the form ID, the latest `lastSubmittedAt` delivered, and every
   response ID delivered at exactly that instant;
2. a poll that lists `timestamp >= lastSubmittedAt` across every page and
   skips the recorded IDs. The cursor advances only after the last page,
   because the listing is unordered;
3. an atomic commit: the cursor advances only after `PrepareTriggerDelivery`
   has recorded every new event of that poll, in the same per-binding store as
   the inbox, locally and hosted;
4. a start position for a new binding: now, or an explicit earlier time;
5. an event ID that decides edits: `formId:responseId` delivers a response
   once; adding `lastSubmittedAt` delivers every edit.

[`examples/response-recorder`](examples/response-recorder) implements this
cursor at the application level: each run returns `nextCursor`, and the next
run passes it back.

## Branches

Every operation uses `defect` for invalid local input, connection
configuration, or connector contract violations, before any request. A `404`
selects `notFound`. A conclusive Google refusal, such as a `403` for an account
that cannot open the form or read its responses, or a `400` for an expired page
token, selects `providerRejected`. A malformed or oversized response, a form or
response ID that differs from the one requested, a repeated question or
response ID, or an answer keyed by a different question selects
`invalidResponse`. `408`, `429`, 5xx, transport failures, and a `403` whose
Google reason is a rate limit return Retry and honor `Retry-After` up to one
hour. `responses.list` counts against Google's per-minute expensive-read quota,
so the retry policy spans six attempts over up to five minutes, which outlasts a
per-minute quota window. Error responses are read with their own 64 KiB bound
and classified by status. Failures carry only a connector-written message and
kind, never Google's error text, answers, or credentials. A credential that
cannot be resolved or refreshed selects `defect`.

## Studio form picker

The manifest declares one read-only Studio command, `listForms`, a `GET` of
`https://www.googleapis.com/drive/v3/files` with the connection's bearer
access token injected by Dex Web. It lists non-trashed files of type
`application/vnd.google-apps.form` across My Drive, shared files, and shared
drives, most recently changed first. The `formPicker` unit saves the `formId`
and `formTitle` outputs. It pages through at most 20 pages, reports a truncated
list, and accepts a pasted form ID or edit link. It rejects a responder link
and explains why. A pasted form saves a blank title.

The `ui/` package builds the credential-safe Studio bundle published as
`connector-ui.tgz` with the Connector release. The repository requires a UI
artifact for any manifest that declares Studio commands or units. The bundle
renders only the connection status and this picker, from the shared
`@superdurable/dex-connectors-react` components.

## Security boundary

- Tokens, client secrets, and service-account keys are `secretString`
  credentials. They never enter Flow input, Attributes, Results, receipts, or
  logs. The Studio frame never receives them.
- The connector sends the token only to the configured endpoint, never follows
  a redirect, and reads at most `maxResponseBytes`.
- Answers and respondent emails are application data. They enter Results and
  the example's Attributes, so treat Dex state as holding personal data.

## Unverified live behavior

These rely on Google's documentation fetched on 2026-10-01, not on a live
account or a real Google Forms request:

- which timestamp the `timestamp` filter compares; the connector and example
  assume `lastSubmittedTime`, as "submitted" suggests;
- whether `responses.list` has any order, and whether a response submitted
  just before a listing can appear only in a later listing;
- the status and reason Google returns for a form the account cannot open
  (`403` or `404`), an unknown response ID, an expired page token, and a rate
  limit, and whether Google sends `Retry-After`;
- that reading responses needs edit access and a viewer's read is a `403`;
- whether item and question IDs are always present, and whether an `isOther`
  option has an empty `value`;
- whether a refreshed or delegated Google token reports all three scopes in
  `scope`;
- the form picker against a real Drive account and the restricted-scope consent
  flow.

## Example

[`examples/response-recorder`](examples/response-recorder) is a runnable Dex
Web **Start Flow** example that uses all three operations. It reads the picked
form's questions, then records each new response's answers by question title,
either every response since an earlier run's cursor or one response by ID.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the example owns its real
Worker, retry, re-dispatch, persistence, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

The re-dispatch test waits for a nine-second fake provider, so the run takes
about fifteen seconds.
