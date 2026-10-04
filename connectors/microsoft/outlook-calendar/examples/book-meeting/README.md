# Outlook Calendar book-meeting example

This example runs one operation-only Flow from Dex Web **Start Flow**, books a
meeting only after checking free/busy, and uses every Outlook Calendar
operation:

1. `RecordMeetingRequest` validates the timed meeting and records it with the
   picked calendar;
2. `CheckAttendeeAvailability` calls `outlookcalendar.NewQueryFreeBusyStep`
   for the attendees over exactly the meeting window, and
   `EvaluateAttendeeAvailability` completes as `busy` when any attendee has
   busy, tentative, or out-of-office time;
3. `PlaceMeetingHold` calls `outlookcalendar.NewCreateEventStep` without
   attendees, so Outlook sends no invitation yet. Every attempt of the Step
   uses one idempotency key, sent as Graph's `transactionId` and looked up
   first, so a re-dispatched Step finds its own hold instead of double-booking;
4. `ReadBackMeetingHold` calls `outlookcalendar.NewGetEventStep`, and
   `VerifyMeetingHold` compares the stored start and end instants with the
   request;
5. `ListMeetingWindowEvents` calls `outlookcalendar.NewListEventsStep`, and
   `EvaluateMeetingWindow` completes as `conflict` when another blocking event
   overlaps the window on the organizer's calendar;
6. `InviteMeetingAttendees` calls `outlookcalendar.NewUpdateEventStep` to add
   the attendees, which makes Outlook send the invitations, and
   `CompleteMeetingBooking` completes as `scheduled`.

The conflict check compares instants, never text: an event whose body says
"2:00 PM PST" but whose instant is 11:00 in Los Angeles does not conflict.
Cancelled meetings and events shown as `free` or `workingElsewhere` do not
block; `unknown` does.

Only the happy-path branches are wired. `incomplete` free/busy, provider
rejection, invalid responses, a missing event, and local defects fail the
Flow, so an attendee whose availability is unknown never looks free. A
conflicting hold stays on the calendar without attendees; this release has no
delete operation, so remove it in Outlook.

## Configure

Follow the [Outlook Calendar Connector setup](../../README.md#authorization),
then configure the `outlook-calendar-scheduler` connection under
**Connections** in Dex Web, with either delegated Microsoft OAuth (Dex CLI
1.4.1 or later) or an app-only mailbox. The **Meeting calendar** picker on the
`PlaceMeetingHold` Step lists calendars the connection can edit and saves the
calendar ID and name; every later Step uses the same calendar. Leave it unsaved
to use the mailbox's default calendar. The Worker reads the saved value once at
startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/microsoft/outlook-calendar` with the
latest stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/book-meeting/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/book-meeting
```

The command must report `valid: true`; inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release. Build release metadata from this source and pass
it as an override, from the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/outlook-calendar-release
(cd connectors/microsoft/outlook-calendar/ui && npm ci && npm run build)
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/microsoft/outlook-calendar/connector.yaml \
  --ui-root connectors/microsoft/outlook-calendar/ui/dist \
  --output /tmp/outlook-calendar-release/connector-ui.tgz \
  --digest-output /tmp/outlook-calendar-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/microsoft/outlook-calendar/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar \
  --version v0.21.0 --tag connectors/microsoft/outlook-calendar/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/outlook-calendar-release/connector-ui.tgz \
  --ui-digest /tmp/outlook-calendar-release/connector-ui.tgz.sha256 \
  --output /tmp/outlook-calendar-release/connector-release.json \
  --digest-output /tmp/outlook-calendar-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/microsoft/outlook-calendar/build" \
  --connector-release-override outlook-calendar=/tmp/outlook-calendar-release
```

Then run the Worker from `connectors/microsoft/outlook-calendar`. It reads the
`DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
go run ./examples/book-meeting
```

The default Worker address is `127.0.0.1:8839`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Graph-compatible fake only,
`OUTLOOK_CALENDAR_LOCAL_PROVIDER_URL` sends every Graph and token request to
one loopback URL; production Workers leave it unset.

Start `OutlookCalendarBookMeeting` with a unique Flow ID:

```json
{
  "subject": "Meridian kickoff",
  "body": "Agenda to follow.",
  "start": {"dateTime": "2026-02-24T09:00:00-08:00", "timeZone": "America/Los_Angeles"},
  "end": {"dateTime": "2026-02-24T10:00:00-08:00", "timeZone": "America/Los_Angeles"},
  "attendees": ["priya@meridian.example.com"],
  "requestOnlineMeeting": true
}
```

The Flow result and the `outlook-calendar-meeting-outcome` Attribute hold the
status, the event, and any busy attendees or conflicting event IDs. A
`scheduled` run sends real invitations; use test mailboxes.

## Test

```bash
GOWORK=off go test -race ./examples/book-meeting/...
```

With the latest Dex development server running, the integration test drives
the Flow on a real Worker against a stateful fake Graph: a scheduled meeting
whose first create response is lost and whose retried Step finds its own hold;
a create that Graph stores at once but answers after nine seconds, so Dex
dispatches a second attempt that finds the hold; a create that Graph commits
only after nine seconds, so the second attempt repeats the `transactionId`, is
rejected, and retries until the lookup finds the one event; a busy attendee
through an app-only connection; a double booking; an unwired `incomplete`
free/busy; and a time without an offset rejected before any provider request.

```bash
GOWORK=off go test -tags=integration ./examples/book-meeting/... -count=1 -v
```
