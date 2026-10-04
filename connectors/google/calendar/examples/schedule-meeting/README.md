# Google Calendar schedule-meeting example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Google Calendar operation:

1. `RecordMeetingRequest` validates the timed meeting and records it with the
   picked calendar;
2. `CheckMeetingAvailability` calls `calendar.NewQueryFreeBusyStep` for exactly
   the meeting window, and `EvaluateMeetingAvailability` completes as `busy`
   when the calendar has busy time;
3. `PlaceMeetingHold` calls `calendar.NewCreateEventStep` without guests, so
   nobody is notified yet. The stable event ID means a retried Step finds its
   own hold instead of double-booking;
4. `ReadBackMeetingHold` calls `calendar.NewGetEventStep`, and
   `VerifyMeetingHold` compares the stored start and end instants with the
   request;
5. `ListMeetingWindowEvents` calls `calendar.NewListEventsStep`, and
   `EvaluateMeetingWindow` completes as `conflict` when another busy event
   appeared in the window after the free/busy check;
6. `InviteMeetingAttendees` calls `calendar.NewUpdateEventStep` to add the
   guests with `sendUpdates: all`, and `CompleteMeetingBooking` completes as
   `scheduled`.

The conflict check compares instants, never text: an event whose description
says "2:00 PM PST" but whose instant is 11:00 in Los Angeles does not conflict,
and transparent and cancelled events are free. An all-day event is read in the
calendar's zone from `ListEventsOutput.TimeZone`.

Only the happy-path branches are wired. `incomplete` free/busy, provider
rejection, invalid responses, a create `conflict`, a missing event, and local
defects fail the Flow. A conflicting hold stays on the calendar without guests;
this release has no delete operation, so remove it in Google Calendar.

## Configure

Follow the [Google Calendar Connector setup](../../README.md), then configure
the `google-calendar-scheduler` connection under **Connections** in Dex Web.
The **Meeting calendar** picker on the `PlaceMeetingHold` Step lists calendars
the connection can edit and saves the calendar ID and name. Every other Step
uses the same calendar. Leave it unsaved to use the authorized account's
`primary` calendar. The Worker reads the saved value once at startup, so
restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/google/calendar` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/schedule-meeting/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/schedule-meeting
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
go run ./examples/schedule-meeting
```

The default Worker address is `127.0.0.1:8830`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `GoogleCalendarScheduleMeeting` with a unique Flow ID:

```json
{
  "summary": "Meridian kickoff",
  "description": "Agenda to follow.",
  "start": {"dateTime": "2026-02-24T09:00:00-08:00", "timeZone": "America/Los_Angeles"},
  "end": {"dateTime": "2026-02-24T10:00:00-08:00", "timeZone": "America/Los_Angeles"},
  "attendees": ["priya@meridian.example.com"],
  "requestConference": true
}
```

The Flow result and the `google-calendar-meeting-outcome` Attribute hold the
status, the event, and any busy intervals or conflicting event IDs.

## Test

```bash
GOWORK=off go test -race ./examples/schedule-meeting/...
```

With the latest Dex development server running, the integration test drives
the Flow on a real Worker against a stateful fake Google Calendar: a scheduled
meeting whose first insert response is lost and whose retried Step finds its
own hold, a busy calendar, a double booking, an unwired `incomplete` free/busy
branch, and a naive time rejected before any provider request.

```bash
GOWORK=off go test -tags=integration ./examples/schedule-meeting/... -count=1 -v
```
