# Google Calendar Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Google Calendar; no live Google account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Google Calendar Connector supplies time as a Flow input. It exposes these
operation-specific Dex Step factories:

- `calendar.NewListEventsStep` lists one page of events on one calendar that
  overlap a bounded window, with recurring events expanded and ordered by start.
- `calendar.NewGetEventStep` reads one event by ID.
- `calendar.NewCreateEventStep` creates one timed or all-day event under a
  stable event ID, optionally with guests and a Google Meet conference.
- `calendar.NewUpdateEventStep` patches the summary, description, start and
  end, or guests of an existing event.
- `calendar.NewQueryFreeBusyStep` returns the busy intervals of 1 to 50
  calendars in a bounded window.

This release has no Triggers.

## Scopes

The OAuth grant requests exactly these scopes, each by its canonical URI:

| Scope | Why |
| --- | --- |
| `https://www.googleapis.com/auth/calendar.events` | `listEvents`, `getEvent`, `createEvent`, and `updateEvent` on every calendar the account can read or edit. |
| `https://www.googleapis.com/auth/calendar.events.freebusy` | `queryFreeBusy` on the account's calendars and on colleagues' calendars that share availability. |
| `https://www.googleapis.com/auth/calendar.calendarlist.readonly` | The Studio calendar picker lists the calendars the account subscribes to. |

These are the narrowest scopes that cover the operations. The full
`calendar` scope would also allow sharing changes and deleting calendars.
`calendar.events.owned` would exclude shared team calendars the account can
edit but does not own. `calendar.freebusy` covers only the account's own
availability, so it cannot answer "is this colleague free". The connector
never deletes events or changes calendar settings or sharing.

A refresh or delegated token that reports fewer scopes requires
reauthorization. Google lets a user uncheck individual scopes on the consent
screen, so the authorization guide asks the user to keep every scope checked.

## Authorization

The connector supports two authorization methods:

- `google-oauth` is recommended for personal Google accounts and ordinary
  Workspace users. Dex requests offline access and refreshes the access token
  before it expires.
- `workspace-domain-delegation` is an administrator-only option. It signs a
  service-account assertion for `delegated_user` with the same three scopes and
  mints a delegated access token. `primary` then means that user's primary
  calendar.

The refresh driver uses `sdkgo/oauthtoken`: the refresh-token grant with
`ClientSecretPost`, `RefreshWhenExpiryMissing`, and an RS256 JWT-bearer
assertion from `SignJWTBearerAssertion` for delegation. `invalid_grant`,
`invalid_client`, and `unauthorized_client` require reauthorization. When
Google omits a new refresh token, the prior one is kept. After a 401 the
connector asks once for a refresh, which the project connection performs only
when the stored expiry has passed, and then retries once; otherwise the 401
selects `providerRejected`. There is never a refresh loop.

The driver never owns persistence. The project connection that
`NewProjectConnection` opens admits one refresh per credential generation
across application replicas and stores the complete replacement before the call
uses it. OAuth client secrets, refresh tokens, and service-account keys stay in
encrypted project storage and never enter a Flow.

## Time

Every time the connector accepts is explicit, and every time it returns is
unambiguous. `EventDateTime` keeps Google's native shape, with exactly one of
`date` and `dateTime`:

- A timed boundary sets `dateTime` to RFC 3339 with a `Z` or `±hh:mm` offset,
  such as `2026-02-24T09:00:00-08:00`, and `timeZone` to an IANA name, such as
  `America/Los_Angeles`. The offset fixes the instant; `timeZone` is the zone
  Google displays. `2026-02-24T09:00:00` has no offset and selects `defect`
  instead of being guessed.
- An all-day boundary sets `date` to `YYYY-MM-DD` and no `timeZone`. The end
  date is exclusive, so a one-day event on 24 February ends on `2026-02-25`.

`ValidateEventTimes` applies the rules `createEvent` and `updateEvent` enforce:
both boundaries are the same kind, and the end is a later instant than the
start even when the two offsets differ. IANA names are validated against the
zone database embedded in the connector, so validation does not depend on the
host. `listEvents` and `queryFreeBusy` require both `timeMin` and `timeMax` with
explicit offsets and a window of at most 366 days.

Returned events carry `isAllDay` and an explicit `transparency`: Google omits
the field for busy events, and the connector reports `opaque`.
`EventDateTime.Instant` converts a boundary to a `time.Time`. An all-day date
needs the calendar's zone, which `ListEventsOutput.TimeZone` reports. The
connector never reads a time from a summary or description, so text such as
"Starts 2:00 PM PST" cannot move an event.

## Idempotent writes

`createEvent` writes under a client-supplied event ID: `eventId` when the
application sets one for business-level deduplication, otherwise the Step's
Call ID without dashes. A UUID's hexadecimal digits are valid Google event ID
characters, and the ID is also the Meet conference request ID. Each attempt
first reads that ID:

- an existing event selects `created` with `wasAlreadyCreated: true` and no
  insert;
- a missing ID is inserted; a duplicate-ID `409` reads the event back;
- a deleted event under the ID, or one the connection cannot read, selects
  `conflict`, because Google does not reuse a deleted event's ID.

Because a repeated attempt finds its own event, a lost connection or 5xx after
the insert is retried rather than reported as uncertain, and the operation has
no `uncertain` branch. Google documents that it cannot always detect a
duplicate ID at insert time, and the pre-read narrows that window further. A
new Step execution is a new logical create; set `eventId` to deduplicate across
executions.

`updateEvent` reads the event, returns it with `wasAlreadyApplied: true` when it
already holds every requested value, and otherwise patches only the requested
fields with `If-Match` set to the read ETag. A retry after a lost response
therefore finds its own change and sends no second patch and no second guest
notification. A `412` from a concurrent edit, a transport failure, or a 5xx is
retried from the read. `start` and `end` change together, and a switch between
timed and all-day clears the other form. A cancelled event selects `notFound`.

## Branches

Every operation uses `defect` for invalid local input, connection
configuration, or connector contract violations, before any provider request.
A conclusive Google refusal, such as a read-only calendar, uses
`providerRejected` with a safe failure that names only a documented Google
reason code. A malformed or oversized response uses `invalidResponse`. `404`
and `410` use `notFound` where the operation declares it. `429`, a `403` with
`rateLimitExceeded` or `userRateLimitExceeded`, `408`, and 5xx responses retry
and honor `Retry-After`; a `403` with `quotaExceeded` is terminal.

`queryFreeBusy` selects `incomplete` when Google reports an error for any
requested calendar, such as `notFound` for a calendar that does not share its
availability. The value still lists every calendar with its busy intervals and
errors. An unwired `incomplete` branch fails the Flow, so an unknown
availability never looks free.

Only the happy-path branch of each operation is required; every other branch is
optional.

## Studio calendar picker

The `calendarPicker` unit lists the calendars from the declared read-only
`listCalendars` command, a `GET` of
`https://www.googleapis.com/calendar/v3/users/me/calendarList` with the
connection's bearer access token injected by Dex Web. It saves the stable
`calendarId` and the display `calendarName`. It lists only calendars the Step's
operation can use: `writer` or `owner` for `createEvent` and `updateEvent`,
`reader` or above for reads, and any calendar for `queryFreeBusy`. A manual
calendar ID field remains, because `queryFreeBusy` and shared calendars can use
IDs outside the account's calendar list.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the connector release. Dex Web renders every Studio
unit, including a manifest-declared picker, through that bundle, and release
metadata requires the artifact whenever `spec.studio` is declared.

## Project connection

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/schedule-meeting/main.go`](examples/schedule-meeting/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := calendar.NewProjectConnection(project, schedulemeeting.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

## Example

[`examples/schedule-meeting`](examples/schedule-meeting) is a runnable Dex Web
**Start Flow** example that uses all five operations: it checks free/busy,
places an uninvited hold, reads it back, lists the window for a double booking,
and only then invites the attendees.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
