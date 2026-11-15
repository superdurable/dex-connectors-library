# Google Drive Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/google/drive/go@v0.25.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/google/drive`.

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

It also declares three Go Triggers that share one durable `changes.list`
poller per connection: `fileCreated`, `fileUpdated`, and `fileTrashed`, each
filtered by an optional direct parent folder and an exact MIME type. See
[File Triggers](#file-triggers).

The connector does not modify, move, share, or delete existing files.

## TypeScript package

The `@superdurable/connector-google-drive` TypeScript package at `v0.2.0` exposes the same four operations and the `fileCreated`, `fileUpdated`, and `fileTrashed` Triggers at behavior parity with Go; its durable `newProjectFileTriggerRunner` shares the Go connector's cursor document and Trigger inboxes byte for byte, so Go and TypeScript replicas can serve one connection. Its [runnable text-copy Flow](typescript/examples/text-copy/README.md) uses a Dex Worker and the static `google-drive-files` connection, and its [file intake Flow](typescript/examples/file-intake/README.md) starts one Flow per created file. Go-recorded provider conformance scenarios, generated OAuth refresh agreement tests, and a real Dex integration test cover this port with a local Drive stand-in. Live Google authorization, API, and `changes.list` polling behavior remain unverified.

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
require reauthorization. A Google 5xx on a read is retryable; an ambiguous
upload 5xx requires read-back. If Google omits a new refresh token, the prior
value is kept. After a read 401 the connector asks once for a refresh and
resends once. After an upload 401, it forces a refresh and retries the Step on
the next attempt; if refresh cannot help, the 401 selects `providerRejected`.
There is never a refresh loop.

The driver never owns persistence. The project connection that
`NewProjectConnection` opens admits one refresh per credential generation
across application replicas and stores the complete replacement before the call
uses it. Refresh tokens, client secrets, and service-account keys stay in
encrypted project storage and never enter a Flow.

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/text-copy/main.go`](go/examples/text-copy/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := drive.NewProjectConnection(project, textcopy.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

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

Drive's [file creation API](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/create)
does not document an idempotency key. `uploadFile` records the SDK's stable
Call ID as a private app property, `dexIdempotencyKey`, and uses the shared
single-dispatch fence. Each attempt reads the prior heartbeat checkpoint before
credentials or provider lookups. Sync Execute durability makes the checkpoint
visible to later attempts. Applications must not override `uploadFile` Execute
durability to async. The operation:

1. reads a checkpointed provider file ID directly when present, or
   [searches by the private app property](https://developers.google.com/workspace/drive/api/guides/search-files)
   for a file from an earlier attempt or an ambiguous accepted upload;
2. sends a multipart upload only when the fence permits a send and the lookup
   finds no file;
3. reads back an ambiguous sent upload before selecting `uncertain`.

An accepted 2xx response with a valid file ID completes and checkpoints that
ID. An unusable 2xx body, dropped response, other 5xx, or 409 triggers
read-back. A confirmed file completes without resending. A transient read-back
failure retries the Step; a conclusive read-back that cannot confirm the
upload selects `uncertain`. A 429, 503, or documented 403 rate-limit response
records a resend checkpoint and retries with the same key. The next attempt
looks up the key before sending. A missing or stale checkpoint never permits a
resend. Read requests reserve up to one second before the observed Step
deadline; the fenced upload uses its shorter write window. These response
classifications follow Google's [Drive error guidance](https://developers.google.com/workspace/drive/api/guides/handle-errors);
the exact write-acceptance timing of a 503 remains unverified against a live
Drive account.

Limits of this design:

- App properties are private to the OAuth client or service account that wrote
  them, so the lookup finds only files created through the same connection.
- Google documents `incompleteSearch` but does not promise immediate indexing
  of app properties after creation. A stale lookup after a 503 is an unverified
  duplicate risk; the fence prevents resends after claimed, missing, or stale
  checkpoints.
- The prior async release had no fence. Existing in-flight calls should be
  reconciled before moving them to this version; a missing old heartbeat
  cannot authorize a resend.
- The content must be at most `maxUploadBytes`, which cannot exceed Google's
  5 MiB multipart limit. Google Workspace MIME types are rejected, so Drive
  never converts an upload.

Google documents `drive.file` as access to files the app creates, and does not
document whether creating a file inside an existing folder that this client
did not create needs more than `drive.file` plus `drive.readonly`. If Google
rejects such an upload, the rejection selects `providerRejected` and nothing
is created; this is not yet verified against a live account.

## Mocks

`drivemock.New` provides one happy-path default per operation, plus
`notFound`, `providerRejected`, `invalidResponse`, `unsupportedContent`,
and `tooLarge` cases where those branches apply. The broad search default
has three pages with an unordered duplicate; the text-copy example scripts a
single exact source. This snippet is from
[`workflow_mock_integration_test.go`](go/examples/text-copy/flow/workflow_mock_integration_test.go):

```go
mock := drivemock.New(t, ConnectionName)
mock.SearchFiles().Respond(drivemock.SearchFilesFound(drive.SearchFilesOutput{
	Files: []drive.FileSummary{{ID: "file_policy", Name: "Ops Policy", MimeType: "application/vnd.google-apps.document"}},
}, nil))
flow, harness := newDriveIntegrationHarnessForConnection(t, mock.Connection())
harness.startWorker(t)
```

The real-Dex test asserts all four call inputs and counts, including the
upload input derived from the source text.

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
selects `defect` on reads; `uploadFile` records a not-sent checkpoint and
retries credential resolution. Only each operation's
happy-path branch is required; every other branch is optional and fails the
Flow when unwired.

## File Triggers

`drive.NewProjectFileTriggerRunner` reads the connection's Drive change log
once per poll for every `fileCreated`, `fileUpdated`, and `fileTrashed`
binding of that connection, and delivers each binding's matching events in
change order. It is built on the `sdkgo/pollingtrigger` kit (sdkgo v0.38.0):

- On first start it stores Drive's
  [`changes.getStartPageToken`](https://developers.google.com/workspace/drive/api/reference/rest/v3/changes/getStartPageToken)
  token as the cursor, so existing files never start a Flow.
- Each poll calls
  [`changes.list`](https://developers.google.com/workspace/drive/api/reference/rest/v3/changes/list)
  with `includeItemsFromAllDrives`, `supportsAllDrives`, `spaces=drive`, and
  `includeRemoved=false`, follows `nextPageToken`, and stores
  `newStartPageToken` when the list is exhausted, as the
  [change guide](https://developers.google.com/workspace/drive/api/guides/manage-changes)
  describes. Google documents that both tokens do not expire.
- Each event is recorded in its binding's project Trigger inbox before the
  shared cursor, `poll-feeds/google-drive/<connection>/fileChanges/head`,
  commits past it. A restarted or replaced replica neither misses nor replays
  a change, and replicas may share one cursor.

Drive change records name no change kind, so the connector derives it from
the [file resource](https://developers.google.com/workspace/drive/api/reference/rest/v3/files)
times against a checkpoint, the latest change time of the previous complete
pass, minus a five-minute lookback for change records that arrive late:

| Trigger | Delivered when | Event ID |
|---|---|---|
| `fileCreated` | the file is not trashed and `createdTime` is after the checkpoint | `<fileId>@created` |
| `fileUpdated` | the file is not trashed, `modifiedTime` is after the checkpoint, and at least one second after `createdTime` | `<fileId>@modified@<modifiedTime ms>` |
| `fileTrashed` | `trashed` is true and `trashedTime`, or the change time when Drive omits it, is after the checkpoint | `<fileId>@trashed@<time ms>` |

Event IDs are stable, so the lookback's repeated events and a refetch after a
crash start no second Flow with `sdkgo.NewDexFlowTriggerTarget`. Drive
populates `trashedTime` only for shared drive items, so a My Drive
`fileTrashed` event is identified by its change time: a later change to a My
Drive file that is already in the trash delivers `fileTrashed` again with a
new event ID. A file that
is created and edited before one poll produces both `fileCreated` and
`fileUpdated`. Permission, sharing, rename-only, and view changes that leave
`modifiedTime` unchanged deliver nothing. A file moved into a watched folder,
a restored file, a permanently deleted file (`removed`), and shared drive
changes deliver nothing. The first checkpoint is the local clock when the
cursor is seeded, so a clock far ahead of Google's can misclassify files
created in the first minutes. The payload, `drive.FileEvent`, carries
metadata only, never content; read content with `drive.NewReadFileTextStep`.

`drive.FileTriggerConfiguration` filters each binding. `folderId` matches
the file's direct parent only, because Drive reports only the direct parent;
subfolders are not searched. `mimeType` matches exactly. Blank fields match
everything, so a blank `fileCreated` binding starts a Flow for every new file
the connection can see, including folders. A binding without saved
configuration is a startup error.

Rate limits pause every binding of the runner: 429 and the 403
`rateLimitExceeded` and `userRateLimitExceeded` reasons honor `Retry-After`
or back off exponentially. A 404 or 410 for a stored page token, or a 400
unless Google names a parameter other than `pageToken`, reseeds the cursor and
logs a WARN record with the reset count; changes between the last
commit and the reseed are not delivered. Two connection settings tune the
poll: `pollInterval` and `pollPageSize` (1 to 500 changes per page).

This snippet is from the runnable
[`examples/file-intake/main.go`](go/examples/file-intake/main.go):

```go
runner, err := drive.NewProjectFileTriggerRunner(project, fileintake.ConnectionName, drive.ProjectFileTriggerRunnerConfig{
	FileCreatedRoutes: []drive.ProjectFileTriggerRoute{{
		BindingName: fileintake.NewFileTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, fileintake.AcceptCreatedFile, fileintake.ResolveFlowID,
			fileintake.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, drive.WithLogger(logger))
```

`drive.WithLogger` receives the runner's records, which carry connector,
connection, feed, binding, and event IDs, never file names, page tokens, or
credentials. The generated per-Trigger factories, such as
`drive.NewFileCreatedTrigger`, poll one binding with an in-memory cursor, so a
restart reseeds and misses changes made while stopped; use the project runner
for durable delivery. `drive.NewFileTriggerRunner` takes an explicit
`pollingtrigger.CursorStore` for tests and applications without project
storage.

The Triggers use the existing `drive.readonly` scope, which `changes.list`
accepts. Under `dexcli dev` with Dex Web's `connections.json`, inboxes persist
in `trigger-inboxes/` but the poll cursor lives in process memory, so a Worker
restart reseeds there; hosted project storage keeps it.

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

The `mimeTypePicker` unit has one `mimeType` output. It lists Google Docs,
Sheets, Slides, Forms, folders, and common upload types from Google's
[MIME type guide](https://developers.google.com/workspace/drive/api/guides/mime-types),
and accepts any other exact lowercase `type/subtype` without parameters.
Blank matches every type. Drive cannot enumerate MIME types per account, so
the list is release-owned and needs no setup command.

The `ui/` package builds the credential-safe Studio bundle published as
`connector-ui.tgz` with the Connector release. The repository requires a UI
artifact for any manifest that declares Studio commands or units, so the bundle
exists only to render the connection status and this picker from the shared
`@superdurable/dex-connectors-react` components.

## Example

[`examples/text-copy`](go/examples/text-copy) is a runnable Dex Web
**Start Flow** example that resolves a file by exact name, reads its text,
uploads a text copy, and reads the copy back.
[`examples/file-intake`](go/examples/file-intake) starts one Flow for every
file created in a configured folder through the durable `fileCreated` runner.

## Python package

The [Python package](python/README.md) implements the same four operations at
`v0.1.0` and includes a runnable Dex Worker and Client
[text copy Flow](python/examples/text_copy). Its OAuth and Workspace token
refresh behavior follows the generated manifest declaration. The provider
conformance scenarios replay Go requests and outcomes; live Google account
authorization remains unverified for the Python port.

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

The file Trigger integration test drives `NewProjectFileTriggerRunner`
against an in-process Drive change log and a real Dex server, through a
runner restart; like Drive, the fake reports `trashedTime` only for shared
drive files. Live Google change-log behavior, including change-record ordering
and the error body Drive returns for a rejected page token, is not verified.
