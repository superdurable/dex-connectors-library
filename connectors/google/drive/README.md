# Google Drive Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Google Drive; no live Google account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Google Drive Connector resolves Drive files by name, reads their metadata
and bounded text, and uploads new files without creating a duplicate when a
Step retries. It exposes operation-specific Dex Step factories:

- `drive.NewSearchFilesStep` finds non-trashed files by exact or prefix name,
  MIME type, and parent folder, newest first, one bounded page at a time.
- `drive.NewGetFileStep` reads the full metadata of one file by ID.
- `drive.NewReadFileTextStep` exports a Google Doc, Sheet, or Slides file, or
  downloads a small text file, as bounded UTF-8 text.
- `drive.NewUploadFileStep` creates one file from text or bytes in an optional
  parent folder.

The connector does not modify, move, share, or delete existing files, and this
release has no Triggers. A Drive change Trigger needs a durable change-cursor
design that is still open.

## Scopes

Both authorization methods request exactly these canonical scopes:

- `https://www.googleapis.com/auth/drive.readonly`
- `https://www.googleapis.com/auth/drive.file`

Most Drive use in a process resolves a human file name to an ID, so search must
see the files a user already has. `drive.file` alone sees only files this
OAuth client created or a user opened with it through a Google Picker, so it
would silently miss the file a Flow is looking for. `drive.readonly` lets
`searchFiles`, `getFile`, `readFileText`, and the folder picker see and read
every file the account can see, and `drive.file` lets `uploadFile` create new
files without the full `drive` scope, which could also modify and delete every
existing file. `drive.readonly` is a Google restricted scope: an External app
stays in Testing with listed test users, uses an Internal Workspace audience,
or completes Google's restricted-scope verification before it is published.

Google reports alias grants under their canonical URIs and Dex Web compares
granted scopes literally, so the manifest names each scope by its full URI.

## Authorization

- `google-oauth` is recommended for personal Google accounts and ordinary
  Workspace users. Dex requests offline access and refreshes the access token
  before it expires.
- `workspace-domain-delegation` is an administrator-only option. It signs an
  RS256 service-account assertion for the configured Workspace user and mints a
  delegated access token. The administrator authorizes the service account
  client ID for the same two scopes.

`CredentialRefreshDriver` delegates both token exchanges to
`sdkgo/oauthtoken`: the refresh-token grant with `ClientSecretPost`, and the
JWT-bearer grant with `SignJWTBearerAssertion`. Google access tokens always
expire, so a token without a recorded expiry is refreshed
(`RefreshWhenExpiryMissing`). `invalid_grant`, `invalid_client`,
`unauthorized_client`, missing refresh material, an invalid service-account
key or delegated user, and a returned scope list without both Drive scopes
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
application startup, as [`examples/text-copy/main.go`](examples/text-copy/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := drive.NewLocalConnection(store, textcopy.ConnectionName)
```

## Search fidelity

`searchFiles` builds one Drive query from the set filters and always adds
`trashed = false`, so a trashed draft with the same name is never returned.

- `nameMatch: exact`, the default, uses Drive's `name =` operator.
- `nameMatch: contains` uses Drive's `name contains`, which Google documents as
  prefix matching of name terms: `Hello` finds `HelloWorld` but `World` does
  not. It is not substring or fuzzy matching.
- `mimeType` matches one Drive MIME type exactly, such as
  `application/vnd.google-apps.spreadsheet`.
- `parentFolderId` matches direct children of one folder, or `root`. Folder
  scoping is the cheapest way to tell a current file from an archived twin.

Values are quoted with Drive's escaping, so a name cannot change the query.
Folder IDs, MIME types, page sizes, and page tokens are validated first; an
invalid value selects `defect` without a request.

Results are ordered by `modifiedTime desc,name`. `pageSize` is 1 to 100; zero
uses the connection's `searchPageSize`. Each page returns the file ID, name,
MIME type, parents, modification time, and web link, plus `nextPageToken` and
Drive's `incompleteSearch` flag. `found` means the page has matches or more
pages remain; Drive may return a short or even empty page while
`nextPageToken` is set. `notFound` is selected only when the page is empty and
no page remains, so an empty search is never a silent success. When
`isIncompleteSearch` is true, Drive did not search every document and an
absent match is not conclusive.

The search, the duplicate lookup, and the folder picker use
`corpora=allDrives` with `includeItemsFromAllDrives` and `supportsAllDrives`,
so they cover My Drive, files shared with the account, and every shared drive
it belongs to. Google may report `incompleteSearch` for that corpus, which is
why the flag is part of the Result. A Google Sheets file's Drive ID is also its
spreadsheet ID, so a Flow can pass a `searchFiles` result directly to the
Google Sheets connector.

The connector reports every match; the application decides what an ambiguous
name means. The example completes with the candidates when more than one file
matches.

## Reading text

`readFileText` reads the file metadata first, then:

- exports Google Docs and Slides as `text/plain` and Google Sheets as
  `text/csv`, which Google limits to the first sheet;
- downloads `text/*` files and JSON, XML, YAML, NDJSON, JavaScript, and SQL
  files whose reported size fits `maxTextBytes`;
- selects `unsupportedContent` without downloading anything for binaries,
  folders, shortcuts, and Google Workspace types without a text export, and for
  content that is not valid UTF-8 or contains a NUL byte;
- selects `tooLarge` when the reported size, the downloaded or exported body,
  or Google's 10 MB export limit (`exportSizeLimitExceeded`) exceeds the bound.

A leading UTF-8 byte order mark, which Google adds to exported text, is removed.
`unsupportedContent` and `tooLarge` still return the file ID, name, and MIME
type. A shortcut's target ID is available from `getFile`.

## Duplicate-safe uploads

Drive has no idempotency-key header, so `uploadFile` records the SDK's
idempotency key as a private app property, `dexIdempotencyKey`, on the file it
creates. The key is derived from the stable Call ID, so every attempt of one
Step execution shares it and a new Step execution gets a new one. Every attempt:

1. looks for a file, trashed or not, that already carries the key, and returns
   the earliest one as `uploaded` with `isFromEarlierAttempt: true`;
2. otherwise sends one multipart upload with the name, MIME type, optional
   parent, key, and content.

A retry after a lost Worker, an Execute timeout, or a rate limit therefore
returns the file the earlier attempt created. A lookup failure never uploads: a
retryable lookup error, or a lookup that Drive reports as incomplete, returns
Retry, and a rejected or invalid lookup selects `providerRejected` or
`invalidResponse`. An upload that Google rejects with 429
or a 403 rate-limit reason returns Retry, because no file was created. A
timeout, dropped connection, 5xx, or invalid success response selects
`uncertain` and is never sent again automatically; route that branch to
operator recovery or read the key back in a later Step.

Limits of this design:

- App properties are private to the OAuth client or service account that wrote
  them, so the lookup finds only files created through the same connection.
- Drive search is eventually consistent. A retry that starts within moments of
  an applied upload may not see it yet; the ambiguous cases above therefore
  select `uncertain` instead of retrying.
- Uploads use async Execute durability like the other operations, because a
  bounded upload is not expected to take seven seconds. An upload that
  outlasts the local phase is sent again by the next attempt, and the lookup
  finds the first file only if Google has already created and indexed it.
  Applications that upload multi-megabyte content over slow links can set
  `StepOptionsOverride` durability to sync.
- The content must be at most `maxUploadBytes`, which cannot exceed Google's
  5 MiB multipart limit. Google Workspace MIME types are rejected, so Drive
  never converts an upload.

Google documents `drive.file` as access to files the app creates, and does not
document whether creating a file inside an existing folder that this client
did not create needs more than `drive.file` plus `drive.readonly`. If Google
rejects such an upload, the rejection selects `providerRejected` and nothing
is created; this is not yet verified against a live account.

## Branches

Every operation uses `defect` for invalid local input, connection
configuration, or Connector contract violations, before any request.
`providerRejected` is a conclusive Google refusal, such as a revoked grant,
missing permission, or unknown parent folder. `invalidResponse` is a malformed
or oversized metadata or search response. Transport failures, 429, 5xx, and 403
rate-limit reasons on reads return Retry, honoring `Retry-After` up to one
hour. Error responses are read with their own 64 KiB bound and classified by
status, so a small `maxTextBytes` never turns a 404 or 503 into `tooLarge`.
Failures carry only a safe message and kind, never provider error text, file
content, or credentials. A credential that cannot be resolved or refreshed
selects `defect`, as in the other Google connectors. Only each operation's
happy-path branch is required; every other branch is optional and fails the
Flow when unwired.

## Studio folder picker

The manifest declares one read-only Studio command, `listFolders`, which lists
non-trashed folders by name with the connection's access token injected by
Dex Web, and one configuration unit, `folderPicker`, with the `folderId` and
`folderName` outputs. A Flow composes the unit in a Step's `ConfigurationUI`
and binds both outputs to its own configuration shape. The unit pages through
at most 20 pages, reports a truncated list, and accepts a pasted folder ID or
`drive.google.com/drive/folders` link for a folder that is not listed; a
pasted folder saves a blank display name. Blank means the operation runs
without a folder.

The `ui/` package builds the credential-safe Studio bundle published as
`connector-ui.tgz` with the Connector release. The repository requires a UI
artifact for any manifest that declares Studio commands or units, so the bundle
exists only to render the connection status and this picker from the shared
`@superdurable/dex-connectors-react` components.

## Example

[`examples/text-copy`](examples/text-copy) is a runnable Dex Web
**Start Flow** example that resolves a file by exact name, reads its text,
uploads a text copy, and reads the copy back.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the same module owns its real
Worker, retry, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
