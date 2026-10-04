# Microsoft Outlook Calendar Connector

> **Verification status: partial live.** Only placeholder-credential probes reached Microsoft, confirming Microsoft Graph's 401 and the identity platform's 400 error shapes; everything else ran on a real Dex stack against a local stand-in. No live Microsoft 365 tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Microsoft Outlook Calendar Connector reads and writes Outlook calendars
through Microsoft Graph v1.0 (`https://graph.microsoft.com/v1.0`) and supplies
time as a Flow input. It exposes these operation-specific Dex Step factories,
named like the [Google Calendar Connector](../../google/calendar)'s so an
application can move between them:

- `outlookcalendar.NewListEventsStep` lists one page of a calendar view: the
  single events, occurrences, and exceptions that overlap a bounded window,
  with recurring series expanded.
- `outlookcalendar.NewGetEventStep` reads one event by its Graph ID.
- `outlookcalendar.NewCreateEventStep` creates one timed or all-day event under
  the Step's idempotency key, optionally with attendees and a Teams meeting.
- `outlookcalendar.NewUpdateEventStep` patches the subject, body, location,
  start and end, or attendees of an existing event.
- `outlookcalendar.NewQueryFreeBusyStep` returns the availability of 1 to 20
  users, distribution lists, or rooms through Graph `getSchedule`.

This release has no Triggers; see [Triggers](#triggers).

## Authorization

The connector supports two authorization methods.

### Delegated Microsoft OAuth (`microsoft-oauth`, default)

One Microsoft 365 work or school user authorizes the connection, and every
call addresses that user's mailbox as `/me`. Dex Web uses Microsoft's static
multi-tenant endpoints,
`https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize` and
`.../token`, because manifest OAuth endpoints cannot name a tenant. The app
registration must therefore support **Accounts in any organizational
directory (multitenant)**; a single-tenant registration fails there with
`AADSTS50194`. Personal Microsoft accounts are out of scope for this release.

Microsoft OAuth needs Dex CLI 1.4.1 or later. Microsoft does not echo
`offline_access` in the token response's `scope`, and Dex Web before
cli-v1.4.1 rejected the callback for that; cli-v1.4.1 (dex#584) accepts a
returned refresh token as proof of `offline_access`.

| Scope | Why |
| --- | --- |
| `offline_access` | Makes Microsoft return a refresh token, so Dex can renew the one-hour access token. |
| `Calendars.ReadWrite` | Every operation and the calendar picker: calendar view, event reads, creates and updates, and `getSchedule`. It needs no administrator consent, though a tenant can require approval of user consent. |

Scopes use Graph's short permission names, which Dex Web compares with the
returned `scope` string. `Calendars.ReadWrite` is the narrowest permission that
covers writes; `getSchedule` alone would need only `Calendars.ReadBasic`. The
connector never deletes events, changes calendar settings, or reads mail.

The refresh driver uses `sdkgo/oauthtoken`: the refresh-token grant at the
`organizations` token endpoint with `ClientSecretPost` and no `scope`, which
Microsoft documents as optional, so the original grant is kept. Microsoft
rotates refresh tokens, so the new one replaces the old; when none is
returned, the prior one is kept. `invalid_grant`, `invalid_client`,
`unauthorized_client`, `interaction_required`, and `consent_required` require
reauthorization, and so does a refreshed grant whose `scope` lacks
`Calendars.ReadWrite` (compared without case, in short or
`https://graph.microsoft.com/` form). The check never requires
`offline_access`. After a 401 from Graph the connector asks once for a refresh,
which the project connection performs only when the stored expiry has passed,
and then retries once; otherwise the 401 selects `providerRejected`. There is
never a refresh loop.

### App-only for one mailbox (`app-only`)

An Entra application authenticates as itself with a client secret, and every
call addresses the one configured mailbox as `/users/{mailbox}`. The driver
repeats the client credentials grant at
`https://login.microsoftonline.com/{tenantId}/oauth2/v2.0/token` with scope
`https://graph.microsoft.com/.default` before each one-hour token expires.
`tenantId` must be a directory GUID or a verified domain; it is validated
before the URL is built, and `common`, `organizations`, and `consumers` are
rejected. A single-tenant registration works here. `invalid_client`,
`unauthorized_client`, `invalid_request` (such as an unknown tenant), and
`invalid_scope` require the credentials to be replaced.

Grant the least privilege an Exchange administrator can scope:

- **Exchange Online application RBAC (recommended).** Register the app's
  service principal with `New-ServicePrincipal`, create a scope such as
  `New-ManagementScope -RecipientRestrictionFilter "PrimarySmtpAddress -eq
  'scheduling@contoso.com'"`, assign `New-ManagementRoleAssignment -Role
  "Application Calendars.ReadWrite" -App <client ID> -CustomResourceScope
  <scope>`, and check it with `Test-ServicePrincipalAuthorization`. Do not also
  grant the Entra application permission: Exchange combines the grants, so an
  unscoped Entra grant reaches every mailbox. Assignments take 30 minutes to
  2 hours to apply.
- **Entra application permission.** `Calendars.ReadWrite` (application) with
  administrator consent reaches every mailbox in the tenant.
- Microsoft is replacing application access policies with RBAC and asks that
  no new ones be created.

### Credential storage

The driver never owns persistence. The project connection that
`NewProjectConnection` opens admits one refresh per credential generation
across application replicas and stores the complete replacement before the call
uses it. Client secrets and refresh tokens stay in encrypted project storage and
never enter a Flow.

## Time

Every time the connector accepts is explicit, and every time it returns is
unambiguous. `EventDateTime` holds `dateTime`, RFC 3339 with a `Z` or `±hh:mm`
offset such as `2026-02-24T09:00:00-08:00`, and `timeZone`, an IANA name such
as `America/Los_Angeles`. `2026-02-24T09:00:00` without an offset, or a
boundary without `timeZone`, selects `defect` instead of being guessed. IANA
names are validated against the zone database embedded in the connector.

Graph's own `dateTimeTimeZone` is a wall time without an offset plus a zone
name. The connector writes the wall time of the instant in `timeZone`, so
Outlook keeps the organizer's zone, but when a daylight-saving change makes
that wall time name two instants, such as 01:30 on 1 November 2026 in Los
Angeles, it writes the UTC wall time instead, so the instant never moves. Every
request sends `Prefer: outlook.timezone="UTC"` and
`Prefer: outlook.body-content-type="text"`; returned times are read in UTC and
rendered with an offset in the operation's `timeZone`, UTC when blank. A
returned Windows zone name means Graph ignored the preference and selects
`invalidResponse`. `originalStartTimeZone` and `originalEndTimeZone` are
passed through as Graph reports them.

`ValidateEventTimes` applies the rules `createEvent` and `updateEvent`
enforce: the end is a later instant than the start even across offsets, and an
all-day event (`isAllDay`) starts and ends at midnight in one zone, as Graph
requires, with an exclusive end. `listEvents` requires a window of at most 366
days and `queryFreeBusy` one shorter than 62 days, Microsoft's documented
`getSchedule` limit. Returned events carry `showAs` explicitly, `unknown` when
Graph omits it. The connector never reads a time from a subject or body.

## Idempotent writes

Graph has no general idempotency key. `createEvent` uses what Outlook events
offer and does not rely on any undocumented behavior:

- Every attempt of one Step execution uses one key: `transactionId` from the
  input when set, for business-level deduplication across Step executions,
  otherwise the Step's Call ID, a UUID.
- Each attempt first looks the key up with Graph's documented
  extended-property filter, `GET {mailbox}/events?$filter=singleValueExtendedProperties/Any(ep: ep/id eq 'String {e8548d82-fea6-423a-bad8-7b6da468e968} Name DexIdempotencyKey' and ep/value eq '<key>')`,
  which covers every event in the mailbox. A match returns `created` with
  `wasAlreadyCreated: true` and sends no POST.
- Otherwise it POSTs the event with the key as Graph's `transactionId`, which
  Microsoft documents as "a custom identifier specified by a client app for
  the server to avoid redundant POST operations in case of client retries",
  and stamps the same key as that extended property.
- A lost response, 429, or 5xx is retried, and the retry finds its own event
  through the lookup. A 400 or 409 is checked the same way before it is
  reported; a 409 without the event is retried, because a concurrent attempt
  may still be committing it.

Microsoft does not document what a repeated `transactionId` returns: whether
the existing event, a 201, a 409, or another error, or for how long it is
remembered. The lookup therefore covers every sequential retry on its own, and
only two attempts that POST concurrently before either commits depend on
`transactionId`. If both were ever created, a later lookup lists the extra IDs
in `duplicateEventIds`. The operation has no `uncertain` branch.

`updateEvent` reads the event, returns it with `wasAlreadyApplied: true` when it
already holds every requested value, and otherwise PATCHes only the requested
fields with `If-Match` set to the read `@odata.etag`. Times compare by instant,
attendees by lowercase address and type, and bodies after Outlook's line-ending
and edge-whitespace conversion. A retry after a lost response therefore finds
its own change and sends no second PATCH and no second meeting update. A 412 or
409 from a concurrent edit, a transport failure, a 429, or a 5xx is retried from
the read. `start` and `end` change together and always send `isAllDay`. A
cancelled meeting selects `notFound`.

Creating an event with attendees makes Outlook send invitations, which cannot be
turned off, and a PATCH that changes attendees sends an update to the changed
attendees, so the example places an uninvited hold first.

## Branches

Every operation uses `defect` for invalid local input, connection
configuration, or connector contract violations, before any provider request;
an app-only connection without its mailbox is one. A conclusive Graph refusal
uses `providerRejected` with a safe failure naming only the HTTP status and a
word-shaped `error.code`, never `error.message`. A malformed or oversized
response, or a `@odata.nextLink` outside `https://graph.microsoft.com/v1.0`,
uses `invalidResponse`. `404` and `410` use `notFound` where the operation
declares it. `429`, `408`, `500`, `502`, `503`, and `504` retry and honor
`Retry-After` up to one hour. Every request carries `client-request-id` set to
the Call ID, and Receipts carry Graph's `request-id`.

`queryFreeBusy` never reports unknown availability as free. A schedule is
unknown when Graph reports an `error` for it (its `responseCode` is kept, its
message never), an item with status `unknown`, an empty or unreadable
`availabilityView`, or no entry at all; any unknown schedule selects
`incomplete`, and an unwired `incomplete` fails the Flow. `isBusy` is true for a
`tentative`, `busy`, or `oof` item, or a `1`, `2`, or `3` slot in the
availability view even when no item explains it; `free` and `workingElsewhere`
are available, as in Graph's own availability view.

Only the happy-path branch of each operation is required; every other branch is
optional.

## Paging and limits

`listEvents` returns at most 250 events per page, 50 by default, with `$select`
bounding every field, and returns Graph's `@odata.nextLink` unchanged as
`nextPageToken`. Graph asks callers to treat that URL as opaque, so the next
call sends it whole after checking that its scheme and host are
`https://graph.microsoft.com` and that it continues a v1.0 `calendarView`.
Graph does not document the page order, so events are sorted by start within
each page. Outlook throttles each app and mailbox pair to four concurrent
requests and 10,000 requests in ten minutes.

## Studio calendar picker

The `calendarPicker` unit lists calendars through one of two declared
read-only commands, injected with the connection's bearer access token by Dex
Web:

- `listCalendars`, a `GET` of `https://graph.microsoft.com/v1.0/me/calendars`,
  for a delegated connection;
- `listMailboxCalendars`, a `GET` of
  `https://graph.microsoft.com/v1.0/users/{mailbox}/calendars`, for an
  app-only connection, with the mailbox read from the saved connection form.

Both send `$select=id,name,canEdit,isDefaultCalendar,owner` and `$top=100`. The
unit saves the stable `calendarId` and the display `calendarName`, shows only
calendars the connection can edit for `createEvent` and `updateEvent`, and keeps
a manual calendar ID field. Because Graph's nextLink must not be taken apart, a
mailbox with more calendars than one page shows a notice pointing to manual
entry. An app-only connection's token exists only after the Worker's first
Graph call stores it, so its picker works from then on. Blank uses the
mailbox's default calendar.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the connector release.

## Triggers

This release has no Triggers. A change-notification Trigger would need:

- Graph's subscription handshake: Graph POSTs a `validationToken` that the
  endpoint must echo as `text/plain` with `200 OK` within 10 seconds, and
  `sdkgo/webhooktrigger` has no handshake hook yet;
- subscription creation and renewal before expiry, at most 10,080 minutes
  (under seven days) for Outlook events, plus a required `clientState` to
  verify, and lifecycle handling;
- or, for polling instead, a delta query with a durable per-binding cursor,
  which `sdkgo` does not provide.

Until then, poll with `listEvents` from a Timer.

## Project connection

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/book-meeting/main.go`](examples/book-meeting/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := outlookcalendar.NewProjectConnection(project, bookmeeting.ConnectionName, connectionOptions()...)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.
`WithLocalProviderURL` sends Graph and token requests to a loopback fake for
local verification only; production Workers leave it unset.

## Example

[`examples/book-meeting`](examples/book-meeting) is a runnable Dex Web
**Start Flow** example that uses all five operations: it checks the attendees'
free/busy, places an uninvited hold, reads it back, lists the window for a
double booking on the organizer's calendar, and only then invites the
attendees.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, and duplicate-dispatch coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

No live Microsoft 365 tenant was used. Placeholder-credential probes
confirmed that Graph answers an invalid token with 401 and
`InvalidAuthenticationToken` plus a `request-id` header, that the client
credentials grant answers an unknown tenant with 400 `invalid_request`, and
that the `organizations` endpoint answers an unknown refresh token with 400
`invalid_grant`. The following live behavior is unverified: delegated consent
and the returned `scope` string with Dex CLI 1.4.1, refresh-token rotation,
Exchange application RBAC scoping, what Graph returns for a repeated
`transactionId`, extended-property filtering latency right after a create,
`If-Match` and its 412 or 409 on event PATCH, IANA zone names accepted on
create and update, all-day events in other zones, `Prefer` handling on every
endpoint, cancelled meetings in `calendarView`, the `getSchedule` error and
availability-view shapes for unshared or unknown schedules, Teams meeting
creation, `$top` on the calendars list, and both picker commands with a valid
token.
