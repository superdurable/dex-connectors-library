# Zoom booked meeting example

This example follows one booked consultation from Dex Web **Start Flow** to
attendance, using every Zoom operation:

1. `RecordBooking` validates the booked slot, an RFC 3339 start with an
   explicit offset plus an IANA time zone, and stores it.
2. `CreateBookedMeeting` runs `createMeeting` once with a waiting room.
3. On `created`, `RecordScheduledMeeting` records the meeting ID and join URL
   after confirming Zoom stores the requested instant and length.
4. `AwaitBookedMeeting` waits durably for the first of: the scheduled end plus
   15 minutes, a **Reschedule meeting** Action, or a **Check attendance now**
   Action.
5. A reschedule runs `MoveBookedMeeting` (`updateMeeting`), reads the meeting
   back with `ReadBackRescheduledMeeting` (`getMeeting`), confirms the stored
   slot in `RecordRescheduledMeeting`, and waits again. A meeting deleted in
   Zoom completes as `cancelled`.
6. At the attendance time, `ListMeetingParticipants`
   (`listPastMeetingParticipants`) and `RecordMeetingAttendance` complete as
   `attended` when anyone other than the host joined, `noShow` when nobody did
   or Zoom has no ended instance, or `attendanceUnknown` when Zoom refuses the
   listing, such as on a free account.

On `providerRejected`, the Flow completes as `rejected` with Zoom's error code;
nothing was created. On `uncertain`, `RecordUncertainMeeting` records the
connector Call ID, and `FindUncertainMeeting` (`listMeetings`) reads one page
of upcoming meetings. `EvaluateUncertainMeeting` adopts the one meeting with
the same topic, start instant, and length that Zoom created after the booking
was recorded. Otherwise the Flow completes as `needsReconciliation` with a
note, and it never creates the meeting again on its own. A slot Zoom stores
differently from the request also completes as `needsReconciliation`.

The `zoom-booked-meeting-phase` and `zoom-booked-meeting` Attributes hold the
phase and record; both Actions require the `zoom-booked-meeting.manage`
permission and appear only while the phase is `scheduled`. Unwired optional
branches, such as `defect`, fail the Flow.

## Run

Generate strict FDG 2.0 from `connectors/zoom` with the latest stable dexcli
release:

```bash
mkdir -p build
dexcli visualize ./examples/booked-meeting/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/booked-meeting
```

The graph is valid; before the first connector release it reports only the
expected `connector_release_required` warning on the five Zoom Steps.

Run `dexcli dev` with that build directory, open Dex Web at
`http://127.0.0.1:8802` rather than `localhost`, open **Connections**, and
authorize `zoom / zoom-scheduler` as described in the
[connector README](../../README.md#zoom-setup). Then start the Worker:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/booked-meeting
```

The default Worker address is `127.0.0.1:8831`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `ZoomBookedMeeting` with a unique Flow ID and a future slot:

```json
{
  "bookingId": "BK-77",
  "topic": "Consultation with Priya",
  "startTime": "2026-10-08T09:00:00-07:00",
  "timeZone": "America/Los_Angeles",
  "durationMinutes": 30,
  "agenda": "Intro call"
}
```

A live run creates a real meeting in the authorizing user's Zoom account;
delete it in Zoom afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/booked-meeting/...
```

With a Dex development server running, the integration tests start a real
Worker and Client against a deterministic Zoom fake:

```bash
GOWORK=off go test -tags=integration ./examples/booked-meeting/... -count=1 -v
```

They cover a meeting created once with its UTC start and join URL; a create
that answers after nine seconds, which outlasts Dex's async local phase, and
is still sent once; a create whose response never arrives, adopted from the
meeting list with one create request; a 5xx create that finds no match and
stops for an operator with one create request; a `3161` rejection; a
reschedule read back and followed by attendance; a nine-second reschedule that
Dex dispatches twice with identical patches; a reschedule of a deleted
meeting; a no-show; the attendance Timer after a one-minute meeting; and a
Worker process killed one second into its create request, whose replacement
reconciles instead of creating again. The Timer test takes about a minute, and
the Worker-loss test builds this example's Worker binary. Set
`DEX_FLOW_SERVICE_ADDRESS` when the server is not at `127.0.0.1:8801`.
