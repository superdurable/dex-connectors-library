# Google Docs Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Google Docs; no live Google account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Google Docs Connector reads a document as bounded Markdown or plain text
together with its revision ID, creates documents in a Drive folder without a
duplicate when a Step retries, and writes text only at the revision a Flow
read. It exposes operation-specific Dex Step factories:

- `docs.NewGetDocumentTextStep` renders the first tab of one document, without
  pending suggestions, as Markdown or plain text with its title and revision ID.
- `docs.NewCreateDocumentStep` creates one Google Doc from a title and optional
  plain-text or Markdown text in an optional Drive folder.
- `docs.NewReplaceDocumentTextStep` replaces the whole body or named text
  placeholders such as `{{effectiveDate}}` in one batch guarded by a revision.
- `docs.NewAppendTextStep` appends plain paragraphs to the end of the body in
  one batch guarded by a revision.

The connector never deletes, shares, or moves documents, and this release has
no Triggers.

## Scopes

Both authorization methods request exactly these canonical scopes:

| Scope | Why |
| --- | --- |
| `https://www.googleapis.com/auth/documents` | `getDocumentText`, `replaceDocumentText`, and `appendText` on every document the account can open, including templates people wrote in Google Docs. |
| `https://www.googleapis.com/auth/drive.file` | `createDocument` and its duplicate lookup. Only the Drive API can create a document inside a folder and tag it with a private app property; Drive's `files.create` accepts `drive`, `drive.appdata`, or `drive.file`. |
| `https://www.googleapis.com/auth/drive.metadata.readonly` | The Studio document and folder pickers list files the account did not create through this client. |

These are the narrowest scopes that cover the operations. `drive.file` alone
would make `documents.get` and `documents.batchUpdate` see only documents this
client created or a user opened with the Google Picker API, which the Studio
picker is not, so a human-written template would be invisible.
`documents.readonly` cannot write, and `documents.create` cannot place a
document in a folder or accept `drive.file`. `drive.metadata.readonly` lets
the pickers list names and IDs without reading or changing any content, where
`drive.readonly` would also allow downloading every file and `drive` would
allow changing and deleting them. Google classifies `documents` as sensitive,
`drive.file` as non-sensitive, and `drive.metadata.readonly` as restricted: an
External app stays in Testing with listed test users, uses an Internal
Workspace audience, or completes Google's restricted-scope verification before
it is published.

Google reports alias grants under their canonical URIs and Dex Web compares
granted scopes literally, so the manifest names each scope by its full URI.

## Authorization

- `google-oauth` is recommended for personal Google accounts and ordinary
  Workspace users. Dex requests offline access and refreshes the access token
  before it expires.
- `workspace-domain-delegation` is an administrator-only option. It signs an
  RS256 service-account assertion for the configured Workspace user and mints a
  delegated access token. The administrator authorizes the service account
  client ID for the same three scopes.

`CredentialRefreshDriver` delegates both token exchanges to
`sdkgo/oauthtoken`: the refresh-token grant with `ClientSecretPost`, and the
JWT-bearer grant with `SignJWTBearerAssertion`. Google access tokens always
expire, so a token without a recorded expiry is refreshed
(`RefreshWhenExpiryMissing`). `invalid_grant`, `invalid_client`,
`unauthorized_client`, missing refresh material, an invalid service-account
key or delegated user, and a returned scope list without all three scopes
require reauthorization. A Google 5xx is always retryable. If Google omits a
new refresh token, the prior value is kept. After a 401 the connector asks once
for a refresh, which the project connection performs only when the stored
expiry has passed, and then resends once; otherwise the 401 selects
`providerRejected`. There is never a refresh loop.

The driver never owns persistence. The project connection that
`NewProjectConnection` opens admits one refresh per credential generation
across application replicas and stores the complete replacement before the call
uses it. Refresh tokens, client secrets, and service-account keys stay in
encrypted project storage and never enter a Flow.

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/policy-publish/main.go`](examples/policy-publish/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := docs.NewProjectConnection(project, policypublish.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

## Reading text

`getDocumentText` sends one `documents.get` with `includeTabsContent=true`,
`suggestionsViewMode=PREVIEW_WITHOUT_SUGGESTIONS`, and a field mask, so the
text and the revision ID come from the same response and pending suggestions,
which are not yet in force, are excluded. It renders the first tab's body:

| Docs content | `markdown` (default) | `plainText` |
| --- | --- | --- |
| Title, subtitle, Heading 1-6 | `#`, `##`, then one `#` per level | the heading text |
| Bulleted and numbered lists | `- ` or `1. ` with four spaces per nesting level; numbering continues per list | `- ` or `1. ` with two spaces per level |
| Tables | pipe table with the first row as header, `\|` escaped, cell paragraphs joined by spaces | cells separated by tabs, one row per line |
| Person, date, and rich-link chips | display text; a rich link becomes `[title](uri)` | display text |
| Footnote references, horizontal rules | `[^1]`, `---` | `[1]`, an empty line |
| Soft line breaks | a line break | a line break |

An ordinary paragraph that starts like Markdown structure, such as `# ` or
`1. `, is escaped. Inline styling, text-run links, images, equations, headers,
footers, footnote text, and tables of contents are omitted, so Markdown is a
reading rendering, not a lossless copy. `hasOtherTabs` reports tabs after the
first, which this release does not read.

`revisionId` is opaque. Google returns it only to a connection with edit
access and guarantees it for 24 hours; it is empty for a viewer. A document
whose JSON exceeds `maxResponseBytes`, or whose rendering exceeds
`maxTextBytes`, selects `tooLarge`, which still reports the title and revision
when they were read.

## Creating documents

Google documents no idempotency key for creating a Doc, and Drive's
pre-generated file IDs are not supported for Google Workspace types or
conversions. `createDocument` therefore uses Drive's `files.create`:
metadata-only for an empty document, or one multipart upload that Drive
converts into a Google Doc from `text/plain` or `text/markdown`. The metadata
carries the title, optional parent folder, and the SDK's idempotency key as a
private app property, `dexIdempotencyKey`. Every attempt:

1. looks for a file, trashed or not, that already carries the key, and returns
   the earliest one as `created` with `isFromEarlierAttempt: true`;
2. otherwise records a Dex heartbeat checkpoint naming the Step's Call ID;
3. sends the create once.

The lookup alone is not a safe read-before-write: a concurrent attempt can
look before the first create lands, and Google does not document that a new
file is searchable at once. The operation therefore runs with sync
durability, so Dex never dispatches a second attempt while one is in flight,
and the checkpoint covers a lost Worker or an Execute timeout: a later attempt
that finds the checkpoint but not the document selects `uncertain` without
sending. A 429 or a 403 rate-limit reason, which Google returns before
creating anything, clears the checkpoint and is retried. A timeout, dropped
connection, 5xx, 408, or invalid success response selects `uncertain`. Route
that branch to a person who checks Drive, as the example does; the connector
never sends the create again. Keep the Step sync: an application override to
async durability lets Dex send a second create after about seven seconds.

App properties are private to the OAuth client or service account that wrote
them, so the lookup finds only documents created through the same
connection. Drive lists Markdown among the formats it converts into Google
Docs; the import media type `text/markdown` matches Drive's documented export
type for Markdown but has not been checked against a live account's
`about.importFormats`. Whether `drive.file` may create a document inside an
existing folder this client did not create is also not verified live; if
Google refuses, the refusal selects `providerRejected` and nothing is created.

## Writing at a revision

`replaceDocumentText` and `appendText` take a `requiredRevisionId`, normally
the `revisionId` of a `getDocumentText` Step, and send one
`documents.batchUpdate` with `writeControl.requiredRevisionId`. Google applies
the batch atomically and only if the document is still at that revision;
otherwise it answers 400 and applies nothing. Because the revision is part of
the Step input, every dispatch of one Step execution, including the backup
dispatch Dex starts when an async attempt passes about seven seconds, carries
the same revision, and once one batch is applied the revision has moved, so
no second batch with that revision can apply. The operations therefore keep
async durability and retry every unconfirmed outcome: a transport failure, a
5xx, or an unreadable 2xx is retried, and the retry recognizes its own write.

Each attempt reads the first tab with suggestions inline, the view Google
requires for edit indexes, and then:

- at the required revision, builds and sends the batch;
- at another revision, or after a 400 that a fresh read shows moved the
  revision, writes nothing and selects the happy branch with
  `wasAlreadyApplied: true` when the document already holds the requested
  text, or `revisionChanged` with the current revision otherwise. A 400 at an
  unchanged revision selects `providerRejected`.

"Holds the requested text" means: for `wholeBody`, the body equals the text;
for `placeholders`, no requested placeholder remains and every non-blank
replacement occurs; for `appendText`, the body's last paragraphs are exactly
the appended text. A collaborator change after the required revision that
happens to produce the same state is reported as already applied. Placeholder
input is rejected when one placeholder contains another or a replacement
contains a placeholder, so this check is unambiguous for the connector's own
writes.

`replaceDocumentText` targets:

- `placeholders` replaces every case-sensitive occurrence of each placeholder
  with `replaceAllText` limited to the first tab, keeping the surrounding
  formatting. Every placeholder must occur in the body at the required
  revision; otherwise the operation selects `placeholderNotFound` with the
  missing ones and writes nothing. `replacements` reports Google's
  `occurrencesChanged` per placeholder.
- `wholeBody` deletes everything but the body's final newline and inserts the
  text as normal-style paragraphs without bullets or text styling.

`appendText` inserts the text before the body's final newline as new
normal-style paragraphs, or as the only paragraphs of an empty document.

Text Google would strip on insertion, such as control characters other than
tab and line feed, `\r`, and private-use characters, selects `defect`, so a
written document compares exactly with the request. Every write returns the
revision after the write from Google's write control, which chains into the
next guarded write without another read.

## Branches

Every operation uses `defect` for invalid local input, connection
configuration, or connector contract violations, before any request.
`providerRejected` is a conclusive Google refusal, such as a revoked grant,
missing permission, or a connection without edit access. `notFound` is a
document the connection cannot see. `invalidResponse` is a malformed or
oversized response that a retry cannot fix. 429, 408, 5xx, and 403 rate-limit
reasons on reads return Retry, honoring `Retry-After` up to one hour. Error
responses are read with their own 64 KiB bound and classified by status.
Failures carry only a safe message and kind, never provider error text,
document content, or credentials. Only each operation's happy-path branch is
required; every other branch is optional and fails the Flow when unwired.

## Studio pickers

The manifest declares two read-only Studio commands that list non-trashed
Drive files with the connection's access token injected by Dex Web, across My
Drive, shared files, and shared drives:

- `listDocuments` filters to the Google Docs MIME type, most recently modified
  first, for the `documentPicker` unit with the `documentId` and
  `documentTitle` outputs;
- `listFolders` lists folders by name for the `folderPicker` unit with the
  `folderId` and `folderName` outputs.

Each unit pages through at most 20 pages, reports a truncated list, and accepts
a pasted ID or `docs.google.com/document` or `drive.google.com/drive/folders`
link for a file that is not listed; a pasted value saves a blank display name.
A Flow composes the units in a Step's `ConfigurationUI` and decides what blank
means there.

The `ui/` package builds the credential-safe Studio bundle published as
`connector-ui.tgz` with the Connector release. The repository requires a UI
artifact for any manifest that declares Studio commands or units, so the bundle
exists only to render the connection status and these pickers from the shared
`@superdurable/dex-connectors-react` components.

## Example

[`examples/policy-publish`](examples/policy-publish) is a runnable Dex Web
**Start Flow** example that reads a policy template, creates a document from
it, fills its placeholders at the revision it read, appends a publication
stamp, and reads the published text back.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the example owns its real
Worker, retry, duplicate-dispatch, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
