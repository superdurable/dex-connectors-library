# Microsoft OneDrive and SharePoint Connector

> **Verification status: partial live.** Only a placeholder-token request reached Microsoft Graph, which returned 401; everything else ran on a real Dex stack against a local stand-in. No Microsoft tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Microsoft OneDrive and SharePoint Connector finds, reads, and writes files
in a user's OneDrive and in SharePoint document libraries through Microsoft
Graph v1.0. Its operations mirror the Google Drive connector's names:

- `onedrive.NewSearchFilesStep` finds files and folders by exact or partial
  name in one folder or across one drive, one bounded page at a time.
- `onedrive.NewGetFileStep` reads the metadata of one file or folder by ID.
- `onedrive.NewReadFileTextStep` downloads one small text file as bounded
  UTF-8 text without exposing its pre-authenticated download URL.
- `onedrive.NewUploadFileStep` writes one file by parent folder and name with
  an explicit `fail` or `replace` conflict behavior.
- `onedrive.NewCreateFolderStep` ensures one folder with a name exists in a
  parent folder.

Every operation takes a `driveId`. Blank means the signed-in user's OneDrive
(`/me/drive`); a SharePoint document library is chosen with the drive picker.
The connector does not move, rename, share, or delete items, and this release
has no Triggers (see [Triggers](#triggers)).

## Authorization

### Microsoft work or school account (default)

`microsoft-oauth` is delegated OAuth: the Flows act with one Entra user's own
access, so they can reach only files and libraries that user can open.

- Manifest OAuth endpoints are static, so the connector signs in through
  `https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize` and
  `.../token`. That endpoint needs a **multitenant** app registration
  ("Accounts in any organizational directory"); a single-tenant registration
  fails there with `AADSTS50194`. The setup guide says so.
- Scopes, in Microsoft's short canonical form so they match the `scope` string
  Microsoft returns:
  - `Files.ReadWrite.All` reads and writes every file the user can open, in
    OneDrive and in SharePoint libraries. `Files.ReadWrite` would reach only
    the user's own OneDrive.
  - `Sites.Read.All` lets the site picker search SharePoint sites and list
    their libraries. Operations do not need it.
  - `offline_access` makes Microsoft return a refresh token.
  None of the three needs administrator consent, but a tenant that blocks user
  consent needs an administrator to grant it.
- Microsoft never echoes `offline_access` in the token response's `scope`.
  Dex Web treats a returned refresh token as proof of it from **Dex CLI 1.4.1**
  (dex#584); older Dex Web releases reject the consent. Use Dex CLI 1.4.1 or
  later for this method.
- Personal Microsoft accounts are out of scope for v0.1.0: they need the
  `consumers` endpoints, have no SharePoint, and report content hashes
  differently. The `organizations` endpoint rejects them.

`CredentialRefreshDriver` refreshes through `sdkgo/oauthtoken`
(`ExchangeRefreshToken`, `ClientSecretPost`) at the organizations token
endpoint and sends the manifest scopes again. Microsoft rotates the refresh
token on every refresh and the driver keeps the replacement. The connector's
own scope check requires only `Files.ReadWrite.All`, never `offline_access`,
and accepts both short and `https://graph.microsoft.com/`-prefixed names
without case. `invalid_grant`, `invalid_client`, `unauthorized_client`,
`invalid_request`, `invalid_scope`, `interaction_required`, and
`consent_required` require reauthorization; a 5xx is always retryable.

### App-only (client credentials)

`microsoft-app-only` is for unattended Flows. The app registration
authenticates as itself with a tenant ID, client ID, and client secret, which
`oauthtoken.ExchangeClientCredentials` exchanges at
`https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token` for
`https://graph.microsoft.com/.default`. This works for single-tenant
registrations. The tenant ID must be a directory GUID or a DNS domain such as
`contoso.onmicrosoft.com`; anything else, including `common` and
`organizations`, requires new credentials before any request. The token is
stored in the connection file with its expiry and renewed five minutes before
it expires or after a 401.

Application permissions need administrator consent. For least privilege:

- **`Sites.Selected`** grants nothing until a SharePoint administrator grants
  the app a role on a site with
  `POST https://graph.microsoft.com/v1.0/sites/{site-id}/permissions`
  (`roles: ["write"]`, the app under `grantedToIdentities`). Microsoft
  documents narrower `Lists.SelectedOperations.Selected` and
  `Files.SelectedOperations.Selected` scopes for one library or folder; they
  are not exercised here.
- `Files.ReadWrite.All` reaches every OneDrive and SharePoint library in the
  tenant; use it only when the Flows need that.

App-only connections have no signed-in user, so a blank `driveId` selects
`defect` without a request. Microsoft documents that drive search and site
search do not support `Sites.Selected`: scope `searchFiles` to a folder, which
lists children instead of searching, and paste the site or drive ID into the
pickers.

For local Dex Web setup, name the factory connection and load the same name at
application startup, as [`examples/text-copy/main.go`](examples/text-copy/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := onedrive.NewLocalConnection(store, textcopy.ConnectionName)
```

Both methods use the generated `NewLocalConnection`, whose refreshing provider
stores renewed tokens. Hosted apps receive only an operation-scoped access
token: `DecodeResolvedCredentialsJSON` rejects refresh tokens and client
secrets, and `DecodeCredentialsJSON` and `EncodeCredentialsJSON` are the
broker's trusted hooks.

## Search fidelity

`searchFiles` picks one Graph request from its input:

- **Exact name in a folder** (`parentFolderId` set, `nameMatch` blank or
  `exact`) reads `GET .../items/{folder}:/{name}`. A folder holds at most one
  item per name, so this is exact and strongly consistent; `found` holds one
  item and `notFound` none. Use `root` for the drive root.
- **Other filters in a folder** list the folder's direct children
  (`.../children`) and keep the page's items whose name contains `name`.
- **No folder** runs Graph's drive search, `GET .../root/search(q='...')`, with
  the name as search text (required). Graph matches names, metadata, and
  content through an index that can lag recent changes, so the connector keeps
  only names that match exactly or contain the text, and the Result sets
  `isIndexedSearch`: an absent match there is not conclusive.

Names compare without case, as OneDrive and SharePoint do. `mimeType` keeps
only files of one media type. Filtering happens per page, so a page can be
short or empty while `nextPageToken` is set; `notFound` is selected only for an
empty last page. `nextPageToken` is Graph's `@odata.nextLink`, accepted only on
the configured Graph origin under `/v1.0/`; pass it back with the same
filters. Search text is quoted as an OData literal, and characters no item
name can contain (`/ \ * < > ? : | # % "`) select `defect` without a request.

## Reading text

`readFileText` reads the item's metadata first. Folders, packages such as
OneNote notebooks, Office and other binary types select `unsupportedContent`,
and a file larger than `maxTextBytes` selects `tooLarge`, without a content
request. Text is `text/*`, JSON, XML, YAML, NDJSON, TOML, JavaScript, SQL, and
`+json` or `+xml` types, plus common text extensions such as `.md` or `.csv`
when Graph labels the file `application/octet-stream`.

`GET .../content` answers `302 Found` with a pre-authenticated download URL.
The connector's HTTP client never follows redirects, so it follows this one
itself: the URL must be absolute HTTPS without user information, it is fetched
**without** the access token, and it is never returned, logged, or put in a
Failure. An expired link (401 or 403 from the storage host) returns Retry, so
the next attempt gets a fresh one. A second redirect or an unusable `Location`
selects `invalidResponse`. Content that is not valid UTF-8 or contains a NUL
byte selects `unsupportedContent`; a byte order mark is kept as content.
Metadata reads always use `$select` without `@microsoft.graph.downloadUrl`,
and the decoder drops that field if Graph sends it anyway.

## Writes that converge under duplicate dispatch

Graph has no idempotency key, so both Mutations rely on addressing by name.

`uploadFile` sends one
`PUT .../items/{parent}:/{name}:/content?@microsoft.graph.conflictBehavior=<fail|replace>`
with the content and its media type. The behavior is required:

- `fail` writes nothing over an existing item. A `409` reads the item at the
  path back: a file with the same size and QuickXorHash as the content selects
  `uploaded` with `isExistingFileIdentical`; any other file, or a folder,
  selects `alreadyExists` with that item.
- `replace` replaces an existing file's content and adds a version. A repeated
  attempt writes the same bytes again.
- Graph's `rename` is not offered: a repeated attempt would create
  `name 1.txt`.

A PUT by path never creates a second file, so every unknown outcome (timeout,
dropped connection, 5xx, an unreadable 2xx) returns Retry, and the operation
declares no `uncertain` branch. A `423 Locked` or a `409` in `replace` mode
also reads the item back; identical content converges, and other content
selects `providerRejected`. A read-back that finds nothing yet, or a file
whose hash Graph has not reported, returns Retry. The connector computes
QuickXorHash from Microsoft's published algorithm; tests check it against
published vectors and an independent port of Microsoft's C# reference.

`createFolder` posts `{"name", "folder": {}}` with
`@microsoft.graph.conflictBehavior: fail` in both the body and the URL,
because the create-folder page shows the body and the driveItem page says the
URL. A `409 nameAlreadyExists` reads the item at the path back: a folder
selects `created` with `isExistingFolder`; a file selects `nameConflict`.

The example's real-Dex tests hold the first PUT or POST response for nine
seconds, past the seven-second async local phase. Dex dispatches the Step
again; with `fail` the second PUT gets `409`, reads back the identical file,
and selects `uploaded`, with `replace` both PUTs store the same bytes, and the
second POST finds the folder. Each ends with one file or one folder.

Limits of this design:

- `fail` cannot tell its own earlier write from another writer's identical
  file; both are reported as `isExistingFileIdentical`.
- SharePoint can rewrite Office documents on upload (property promotion), so
  their stored hash may differ from the content. A repeated `fail` upload of
  such a file may select `alreadyExists`; use `replace` for Office files.
- The `@microsoft.graph.conflictBehavior` query parameter for PUT is
  documented on the driveItem resource page and on Microsoft's retired
  OneDrive API page, but not on the current upload page; it is not verified
  against a live tenant.
- Content is at most `maxUploadBytes` (5 MiB), because it travels through Flow
  state; Graph's simple upload accepts up to 250 MB.

## Branches

Every operation uses `defect` for invalid input, connection configuration, a
credential that cannot be resolved, or a contract violation, before any
request. `providerRejected` is a conclusive Graph refusal, such as `403`, a
missing parent folder, a blocked file type, or `507` (a full quota, kind
`QUOTA_EXHAUSTED`). `invalidResponse` is a malformed or oversized response.
`408`, `429`, `5xx` other than `501` and `507`, and the codes
`activityLimitReached`, `throttledRequest`, and `serviceNotAvailable` return
Retry, honoring `Retry-After` up to one hour. A 401 forces one coordinated
refresh and one resend. Errors are classified from the status and `error.code`
only; Failures never carry Graph's `error.message`, content, or credentials.
Only each operation's happy-path branch is required; every other branch is
optional and fails the Flow when unwired.

## Studio pickers

The manifest declares three units that a Flow composes in one Step's
`ConfigurationUI`, saving into one configuration object:

- `sitePicker` searches SharePoint sites by keyword and writes `siteId` and
  `siteName`. Blank means the user's own OneDrive.
- `drivePicker` reads `siteId` as an input and lists that site's document
  libraries, or offers the signed-in user's OneDrive when no site is saved. It
  writes `driveId` and `driveName`.
- `folderPicker` reads `driveId`, lists the drive root's folders, and opens a
  selected folder to list its subfolders. It writes `folderId` and
  `folderName`. Blank means the drive root.

Save the units in order, because each one reads the value the previous one
saved. Every unit also accepts a pasted ID, and lists stop after 20 pages with
a notice. The five read-only Studio commands are fixed `GET`s on
`https://graph.microsoft.com/v1.0` with the connection's `access_token` as the
bearer credential and a fixed `$select`:

| Command | Request |
| --- | --- |
| `searchSites` | `/sites?search=` |
| `getMyDrive` | `/me/drive` |
| `listSiteDrives` | `/sites/{siteHostname},{siteCollectionId},{siteWebId}/drives` |
| `listRootFolders` | `/drives/b!{driveKey}/root/children` |
| `listFolderChildren` | `/drives/b!{driveKey}/items/{itemId}/children` |

Dex Web accepts a Studio path parameter only when it matches
`^[A-Za-z0-9._~-]+$`. A Graph site ID (`hostname,GUID,GUID`) contains commas
and a OneDrive for work or school or SharePoint drive ID starts with `b!`, so
neither can travel whole. The URLs therefore keep the commas and the `b!`
prefix literal and take the parts as parameters; the bundle splits the IDs.
Paging passes Graph's `$skiptoken`, read from an `@odata.nextLink` on
`graph.microsoft.com` only. A drive ID without the `b!` shape cannot be
browsed, and the folder unit asks for a pasted folder ID instead.

With app-only connections the pickers work only after the application has
stored its first token, `getMyDrive` fails because there is no signed-in user,
and `searchSites` fails under `Sites.Selected`; paste the site or drive ID,
which Graph Explorer shows for `GET /sites/{hostname}:/sites/{name}:/drives`.

The `ui/` package builds the credential-safe Studio bundle published as
`connector-ui.tgz` with the release, from the shared
`@superdurable/dex-connectors-react` components.

## Triggers

This release has no Triggers. A "file created or changed" Trigger needs one
of two Graph mechanisms, and neither fits the SDK yet:

- **Change notifications.** A subscription on a drive root delivers only an
  `updated` notification that says something changed, without the item. Graph
  first validates the notification URL by sending a `validationToken` that
  the endpoint must echo, and `webhooktrigger` has no handshake hook. The
  subscription also expires and must be renewed before its expiry, which needs
  a scheduler that owns the subscription per binding.
- **Delta queries.** `GET /drives/{id}/root/delta` returns changed items and an
  `@odata.deltaLink` cursor for the next poll, and answers `410 Gone` with a
  fresh enumeration link when the cursor is too old. A poller needs that
  cursor durably stored per binding, which `sdkgo` does not provide, plus
  provider-stable event IDs (item ID plus `cTag` or `eTag`).

A notification would in practice wake a delta query, so both pieces are needed
for push delivery.

## Not in this release

- Personal Microsoft accounts (see [Authorization](#authorization)).
- Large uploads through upload sessions, moves, renames, copies, sharing
  links, permissions, deletes, versions, and check-in or check-out.
- Office conversions such as `?format=pdf` and reading Word or Excel content;
  the Excel connector owns workbook data.
- National clouds (`graph.microsoft.us`, `microsoftgraph.chinacloudapi.cn`),
  whose sign-in endpoints differ from the manifest's static ones.

## Example

[`examples/text-copy`](examples/text-copy) is a runnable Dex Web
**Start Flow** example that looks up a text file by name in a source folder,
ensures a copy folder in a destination folder, copies the text into it with
`fail` or `replace`, and reads the copy back. Both locations come from the site,
drive, and folder pickers.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

The UI links `sdk/react`, so build it first (`make react-sdk` from the
repository root). With the latest Dex development server running, the example
owns its real Worker, retry, persistence, and duplicate-dispatch coverage
against the stateful fake in `internal/graphfake` (each duplicate test takes
about nine seconds):

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
