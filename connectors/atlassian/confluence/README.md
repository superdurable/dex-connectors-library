# Confluence Cloud Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Confluence; no live Atlassian site was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module searches, reads, publishes, updates, and comments on Confluence
Cloud pages through Atlassian's OAuth 2.0 (3LO) gateway,
`https://api.atlassian.com/ex/confluence/{cloudId}/wiki`. Pages, spaces, and
footer comments use REST API v2 (`/wiki/api/v2`); CQL search uses the REST API
v1 content search (`/wiki/rest/api/content/search`), because v2 has no CQL
endpoint.

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `searchPages` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getPage` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createPage` | Mutation | sync | `created` | `titleConflict`, `notFound`, `providerRejected`, `defect` |
| `updatePage` | Mutation | async | `updated` | `versionConflict`, `notFound`, `providerRejected`, `defect` |
| `addComment` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Results, receipts, failures, and logs never contain Confluence's error text or
any token. A rejection names only the HTTP status, such as `Confluence rejected
the page with HTTP 400`. Page bodies are returned only as converted Markdown or
plain text, cut at a caller-chosen character limit.

No Trigger ships in this release. Confluence Cloud webhooks are registered in
an Atlassian Connect app descriptor, and neither the v1 nor the v2 REST API has
a webhook endpoint an OAuth 2.0 (3LO) connection could call, so a connection
cannot subscribe to page events. Poll with `searchPages` instead.

## Atlassian setup

Create one OAuth 2.0 (3LO) app at
<https://developer.atlassian.com/console/myapps/>:

1. **Create > OAuth 2.0 integration**, name the app, and accept the terms.
2. **Permissions > Confluence API > Add**, then **Configure**:
   - under **Classic scopes**, add `search:confluence`, which the v1 CQL search
     requires;
   - under **Granular scopes**, add `read:page:confluence`,
     `write:page:confluence`, `read:space:confluence`,
     `read:comment:confluence`, and `write:comment:confluence`, which the v2
     endpoints require.

   The connection also requests `offline_access`, which makes Atlassian issue
   a rotating refresh token.
3. **Authorization > OAuth 2.0 (3LO) > Configure**: paste the Redirect URI
   Dex Web shows into **Callback URL** and save.
4. **Settings**: copy **Client ID** and **Secret** into the Dex Web form.
5. Choose **Connect**, pick the Confluence site on the consent screen, and
   accept. The app is private to its owner until **Distribution > Enable
   sharing** is on.

Dex requests `audience=api.atlassian.com` and `prompt=consent` and does not
use PKCE, which Atlassian's 3LO flow does not document. `CredentialRefreshDriver`
refreshes the access token five minutes before the `expires_in` Atlassian
reports, through `sdkgo/oauthtoken` with a JSON body, the client presented by
`client_secret_post`, and a response without `token_type` accepted. Atlassian
disables every refresh token when it issues the next one, so the driver always
stores the replacement. Atlassian answers an invalid or expired refresh token
with HTTP 403 and `invalid_grant`, and the driver then reports
reauthorization; a refresh token unused for 90 days expires. A refresh whose
`scope` lacks any requested Confluence scope also requires reauthorization.

### Choosing the site

Every operation works on one site, named by the non-secret `cloudId`
configuration (a UUID). The manifest renders the field with the `sitePicker`
unit, which lists the authorization's sites through the read-only
`listAccessibleSites` command
(`GET https://api.atlassian.com/oauth/token/accessible-resources`).

When `cloudId` is blank, the connector calls the same endpoint before its first
operation and uses the only site whose scopes include a Confluence scope. It
selects `defect` when the grant covers no Confluence site or several. The
resolved site is cached until the application restarts.

Two Dex Web gaps, which the Jira connector documents for `cli-v1.1.0` and
which were not re-checked with the pinned release, affect the picker, so the
unit always offers manual entry of the `cloudId` found at
`https://<your-site>.atlassian.net/_edge/tenant_info`:

- Dex Web renders a connection field's `studioUnit` as a plain text field, so
  the connection form shows `cloudId` as text.
- Dex Web accepts only a JSON object from a Studio command, and Atlassian
  answers `accessible-resources` with a top-level array, so the command fails
  with `CONNECTOR_PROVIDER_COMMAND_FAILED`.

### Choosing a space

The `spacePicker` unit lists current spaces through the read-only `listSpaces`
command (`GET https://api.atlassian.com/ex/confluence/{cloudId}/wiki/api/v2/spaces`,
sorted by name, 100 a page). Confluence answers with a JSON object, so the
command fits Dex Web's limits. The host stays fixed; only the `cloudId` path
segment and the `cursor` query parameter are declared, and the unit follows
the cursor in the `_links.next` URL. It stores `spaceId`, `spaceKey`, and
`spaceName`, and always accepts a typed space key. When the host reports no
connection `cloudId`, the unit asks for one used only for listing. Studio
commands send the stored access token, which Dex Web does not refresh; a
running Worker refreshes and persists it, and otherwise **Reconnect** issues a
new one.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "confluence",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence",
    "moduleVersion": "v0.1.0",
    "provider": "atlassian",
    "connectionName": "confluence-policies",
    "configuration": {"cloudId": "1324a887-45db-1bf4-1e99-ef0ff456d421"},
    "credentials": {"oauth_client_id": "...", "oauth_client_secret": "...", "access_token": "...", "refresh_token": "..."}
  }]
}
```

Load it with `localconfig.LoadFromEnvironment` and
`confluence.NewLocalConnection(store, "confluence-policies")`, as
[`examples/publish-policy/main.go`](examples/publish-policy/main.go) does.
Credentials are reread and refreshed before every provider call; `cloudId`,
`endpoint`, and `maxResponseBytes` are startup configuration. In
Superverse-hosted deployments, construct the client with
`hostedconfig.NewCredentialProviderFromEnvironment(confluence.ConnectorID,
connectionName, confluence.DecodeResolvedCredentialsJSON)`, which accepts only
`access_token`.

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout. Redirects are never followed.
After a 401, the connector refreshes the credential once and resends once,
because Confluence rejects an unauthenticated request before acting on it. A
response that contains the access token is never returned. 408, 429, 5xx, and
transport failures of reads retry, honoring `Retry-After`.

### searchPages

`PageSearchFilter` turns typed values into escaped CQL that always starts with
`type = page`, so no caller text can change the query. The example searches its
space for the policy title:

```go
func MapToSearchPagesInput(request PublicationRequest) confluence.SearchPagesInput {
	return confluence.SearchPagesInput{
		Filter:   confluence.PageSearchFilter{SpaceKeys: []string{request.SpaceKey}, TitlePhrase: request.Title},
		PageSize: maximumRelatedPolicies,
	}
}
```

It sends `type = page AND space in ("OPS") AND title ~ "\"Remote work policy\""
ORDER BY lastmodified DESC`. Every set field adds one clause:

- `SpaceKeys`: `space in (...)`, such as `OPS` or a personal `~...` key.
- `TitlePhrase`: `title ~` with the phrase quoted once for text search and
  once for CQL. Text search ignores case and punctuation.
- `Labels`: `label in (...)`, pages with at least one of the labels.
- `ModifiedSince`: CQL compares `lastmodified` dates in the authorizing user's
  Confluence time zone, so the connector sends the date half a day earlier and
  removes pages modified before the exact instant from each result page. A page
  can therefore hold fewer results than `PageSize`.
- `AdditionalCQL`: one caller-built clause ANDed in parentheses, at most 4096
  characters. Its parentheses and quotes must balance outside string literals
  and it cannot contain `ORDER BY`, so it cannot widen the query beyond pages
  or the typed clauses. Quote outside values with `confluence.QuoteCQLString`.
- `Order`: `lastModifiedDescending` (the default), `createdDescending`, or
  `titleAscending`.

`PageSize` is 1 to 100 (zero requests 25), and `NextCursor` continues the same
filter from the cursor in Confluence's `_links.next`. Results carry the page ID,
title, status, space ID and key, version number, last-modified time, and web
URL, never the body. Search is eventually consistent: a page created or changed
seconds ago can be missing, so never use search to decide whether a write
happened.

### getPage

`GetPageInput` names a numeric `PageID` and reads `GET /pages/{id}` with the
`storage` body (the default) or `atlas_doc_format`. The Value carries the title,
status, space, parent, author, creation time, the current `Version.Number`, and
`Body` converted to Markdown (the default) or plain text, cut at
`MaxBodyCharacters` (1 to 131072, zero keeps 32768) with `IsBodyTruncated`.

The storage reader keeps headings, paragraphs and line breaks, bold, italic,
strikethrough, code, links, lists, task lists, block quotes, tables, rules, the
`code` and `noformat` macros as fenced code, the `info`, `note`, `warning`,
`tip`, and `panel` macros as block quotes, other macros' rich-text bodies, page
links by their text or title, and the status macro's title. Images, mentions,
and macros without a readable body are dropped. The Atlassian Document Format
reader keeps the same structure, mentions, emoji, dates, and cards.

### Writing text

`createPage`, `updatePage`, and `addComment` take a `Body` of at most 262144
characters and a `BodyFormat`:

- `markdown` (the default) is a subset: ATX headings, paragraphs, bullet,
  ordered, and task lists with nesting, block quotes, fenced code, pipe
  tables, rules, and bold, italic, strikethrough, code, and link spans. A
  single line break inside a paragraph is kept as a line break, unlike
  CommonMark. Only `http`, `https`, and `mailto` links become links. Fenced
  code becomes a `<pre>` block, which keeps no language.
- `plainText` splits paragraphs on blank lines and keeps other line breaks;
  nothing is markup.

The connector writes Confluence storage format itself and escapes every text
value, so caller text can never become an element or a macro. Markdown that
`getPage` returns for content in this subset, other than a fence language,
parses back to the same structure.

### createPage

`CreatePageInput` names exactly one of `SpaceID` (as the space picker stores
it) and `SpaceKey`, which the connector looks up first; an optional
`ParentPageID`, where blank uses the space homepage; a one-line `Title` of at
most 255 characters; and the body.

Confluence has no idempotency key, but it keeps one page per title in a space:
[CONFCLOUD-45279](https://jira.atlassian.com/browse/CONFCLOUD-45279), "Allow
creation of page with the same name within the same space", is gathering
interest, and [CONFCLOUD-2524](https://jira.atlassian.com/browse/CONFCLOUD-2524)
was closed Won't Fix. The operation relies on that rule:

| Outcome | Result |
| --- | --- |
| 200 with a valid page | `created` |
| 400 or 409, and a page with the title exists | `titleConflict` with that page's ID and version, or `created` when it is this Step's page |
| 400 or 409, and the title lookup is unavailable or unreadable | Retry after at least five seconds |
| 400 or 409 without such a page or with the lookup refused; 401, 403, 413, or another 4xx except 404, 408, and 429 | `providerRejected` |
| 404, or an unknown space key | `notFound` |
| 429 | Retry after `Retry-After` |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 3xx, 408, 5xx, or an unusable 2xx body | look the title up now: this Step's page selects `created`; otherwise Retry after at least five seconds |

A page found by title is this Step's page only when it was created no earlier
than a minute before the Step's first attempt, sits below the requested parent,
and has the requested content, compared as text without markup. Otherwise the
result is `titleConflict`, and an application that publishes a page by title
updates the page it names, as the example does. `IsConfirmedByTitleLookup`
reports a `created` found this way.

Before sending, the operation records a Dex heartbeat checkpoint with the send
time. A later attempt of the same Step execution, such as the retry after a
lost Worker, finds it and looks the title up first: it reports the page it
finds and sends again only when no page has the title, which Confluence then
refuses to duplicate. A 429 or a provably unsent request clears the
checkpoint. Dex sends the heartbeat without waiting for it to persist; when a
checkpoint is lost that way, the retry's create fails on the title and the
lookup reports the page as `created`, or as `titleConflict` when it cannot
attribute it, never a second page.

### updatePage

`UpdatePageInput` names the `PageID`, the `Title` after the update, the
complete new body, and `NextVersionNumber`, which Confluence requires: the
version number `getPage` returned plus one. An optional one-line
`VersionMessage` of at most 200 characters is shown in page history. The
connector appends a marker derived from the Step execution's call ID, such as
`[dex:3f9a2c1b7e04d5a6]`, and uses it to recognize its own version.

Confluence accepts each version number once, so a repeated update cannot apply
twice; it is refused. The v2 API documents 409 only for spaces that require
approval before publishing, so the connector reads the page after any 400 or
409 instead of trusting the status:

| Page after the refusal | Result |
| --- | --- |
| At `NextVersionNumber` with this Step's marker, or later with that version carrying the marker | `updated`, `IsConfirmedByReadBack` |
| At `NextVersionNumber - 1` | `providerRejected`: Confluence refused the content itself |
| At any other version | `versionConflict`, whose `VersionNumber` is the current version |

After a timeout, dropped connection, 408, 5xx, or unusable 2xx body, the same
read decides; an unchanged page retries after at least five seconds, because
sending the same version again is safe. 404 selects `notFound`; 401, 403, 413,
and other 4xx select `providerRejected`.

### addComment

`AddCommentInput` names the `PageID` and the body and sends one footer comment
with `POST /footer-comments`. Confluence has no idempotency key and allows any
number of identical comments, so the operation sends at most once per Step
execution:

1. It runs with sync durability.
2. Before sending, it records a Dex heartbeat checkpoint with the send time. If
   Dex does not record it, nothing is sent and the attempt retries.
3. A 429 or a connection that never opened clears the checkpoint and retries.
4. A timeout, dropped connection, 3xx, 408, 5xx, or unusable 2xx body keeps the
   checkpoint and retries after at least five seconds. Every later attempt
   finds the checkpoint and never sends: it reads the page's 25 newest footer
   comments and selects `added` with `IsConfirmedByReadBack` when one created
   since a minute before the first attempt has the same text, or `uncertain`.
5. 404 selects `notFound`, and 400, 401, 403, 413, and other conclusive 4xx
   select `providerRejected`.

An application handles `uncertain` without adding the comment again
automatically; the example records the comment as unknown and completes.

## Duplicate dispatch

With async durability, Dex runs a Step in a local phase of about seven seconds
and then dispatches a backup attempt, so a slow write is sent a second time
while the first is in flight. A real Dex run with a Confluence fake that held
each write response for nine seconds recorded:

| Operation | Async durability | Sync durability |
| --- | --- | --- |
| `createPage` | two creates, one page, `created` | one create, one page |
| `addComment` | two comments | one comment |
| `updatePage` | two updates, one new version, `updated` | not needed |

`updatePage` keeps the async default: the backup attempt's update is refused
and finds its own version, and
`TestSlowUpdateBackupAttemptConvergesOnItsVersionWithRealDex` guards this.
`addComment` must stay sync, because only the heartbeat checkpoint stops a
second comment and a backup attempt cannot see the local attempt's checkpoint;
`TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex` guards this.
`createPage` stays sync even though the fake kept one page under async: a
backup attempt sends its create concurrently, and whether Confluence enforces
one page per title atomically under concurrent creates is not documented.
`TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex` guards this. Do not
override these Steps to async.

## Not in this release

- Triggers, because Confluence webhooks are app-level (see above).
- API-token (HTTP Basic) authentication, which Studio setup commands cannot
  send.
- Blog posts, inline comments, comment replies, drafts, labels and properties
  as writes, attachments, moves, and deletes.
- Writing raw storage format or Atlassian Document Format.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example owns its real
Worker, retry, RPC, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/atlassian/confluence/connector.yaml
go run ./cmd/connectorctl generate --check connectors/atlassian/confluence/connector.yaml
(cd connectors/atlassian/confluence/ui && npm ci && npm test && npm run build)
```

The deterministic fakes cover every branch above, CQL escaping and injection
attempts, Markdown and plain-text conversion to storage, storage and Atlassian
Document Format conversion to Markdown and plain text, hostile and malformed
bodies, one refresh after a 401, a second 401, rate limits with `Retry-After`,
redirects, oversized responses, token reflection, site resolution, title
reconciliation, version markers, comment read-back, and the refresh driver's
JSON grant, rotation, 403 `invalid_grant`, outage, and scope checks.

No live Atlassian credentials were used. The following live behavior is
unverified: Dex Web's form-encoded authorization-code exchange against
Atlassian's token endpoint, which documents JSON only; whether the returned
`scope` lists every requested scope, including `offline_access`, which Dex Web
requires; mixing classic and granular scopes in one grant; a
`http://127.0.0.1` Redirect URI in the developer console; the refresh exchange
and 403 `invalid_grant` shape; real response shapes of content search, pages,
page lookup by title, page versions, spaces, and footer comments; whether
`GET /pages?title=` matches titles exactly and immediately after a create;
whether Confluence enforces one page per title atomically and case-sensitively;
the status Confluence returns for a stale version number; the `Atl-Traceid`
response header; how Confluence rewrites written storage, which reconciliation
compares as text; CQL `lastmodified` time zone handling and `expand=space,version`
on content search; 429 and 5xx behavior under load; the `accessible-resources`
and `spaces` commands through Dex Web; and the developer-console paths in the
setup guidance.
