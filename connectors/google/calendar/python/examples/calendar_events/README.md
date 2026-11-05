# Google Calendar Python event listing example

Create a Dex Web connection named `google-calendar-events-reader` using Google OAuth or Workspace domain-wide delegation. The connection requires the Calendar Events, Events FreeBusy, and Calendar List Readonly scopes documented in the connector manifest. Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run `python -m examples.calendar_events.main` from `connectors/google/calendar/python`.

Start `GoogleCalendarEventsPython` with a bounded window such as `{"calendarId":"primary","timeMin":"2026-02-24T00:00:00Z","timeMax":"2026-02-25T00:00:00Z"}`. The Flow persists the listed event page and completes. Other branches are optional and unwired, so a Calendar refusal fails the Flow.

The integration test uses a real Dex Worker and Client and a local provider stand-in. Live Google authorization and provider behavior were not tested.
