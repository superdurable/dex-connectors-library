# Google Calendar TypeScript connector

Install `@superdurable/connector-google-calendar@0.2.0` with `@superdurable/connectors-sdk@0.12.0`, which provides the poll Trigger kit. The package exposes `newListEventsStep`, `newGetEventStep`, `newCreateEventStep`, `newUpdateEventStep`, and `newQueryFreeBusyStep`, plus typed inputs, outputs, mock outcomes, `newProjectConnection`, and the `eventChanged` Trigger.

The connector uses the static Dex Web connection name `google-calendar` in its [runnable meeting example](examples/schedule-meeting/README.md). The example reads an event through a real Dex Worker. Other operations use the same connection. `createEvent` writes under a stable event ID, and `updateEvent` reads before patching so retries do not repeat an already applied write.

Google OAuth and Workspace domain-wide delegation use the manifest-generated refresh driver and private/shared credential decoders. The project connection keeps credentials in host-owned storage and refreshes them before use. Set up the Google Cloud project, Calendar API, scopes, redirect URI, and consent screen using the [connector authorization guide](../README.md#authorization). Keep secrets out of Flow inputs and results.

## eventChanged Trigger

`eventChanged` polls one calendar with Google Calendar incremental sync and delivers one `EventChangedEvent` (`calendarId`, `changeKind`, and the `event`) per created, updated, or cancelled event, at parity with the Go connector; the [connector README](../README.md#eventchanged-trigger) describes the baseline, change kinds, recurring events, expired sync tokens, and rate limits. The binding's sync cursor document and Trigger event IDs are byte for byte the Go connector's, and a change a Go replica recorded in the inbox replays in TypeScript.

- `newProjectEventTriggerRunner(project, connection, routes)` is the durable path: it loads each binding's saved calendar (an unsaved binding watches `primary`), records every change in the binding's project Trigger inbox, and commits the sync token in the binding's project poll cursor only after that. Pass the connection from `newProjectConnection` that the Flow also uses.
- `newEventTriggerRunner(connection, routes)` takes targets and cursor stores directly; a route without a cursor keeps it in memory.
- `newEventChangedTrigger(config)` is one binding's `TriggerRunner` with an in-memory cursor, as Go's generated `NewEventChangedTrigger` is. A memory cursor records a new baseline on every start, so use it only for tests and demonstrations.
- `defineEventChangedTriggerBinding` declares the binding in a Flow's `getConnectorTriggerBindings`; the `calendarPicker` unit saves `calendarId` and `calendarName`.

The connection's `pollInterval` (a Go duration of at least 5 seconds) sets the delay between polls. A 429 or a Google usage-limit 403 pauses every binding of the connection, and a 410 Gone records a new baseline and logs a WARN record. Records carry binding identity, never event payloads or credentials.

[`examples/event-changed`](examples/event-changed/README.md) is the runnable Trigger example: the `calendar-event-changes` binding of the static `google-calendar-watcher` connection starts one `GoogleCalendarEventChangeTypeScript` Flow per change, which reads a created or updated event back with `newGetEventStep`.

## Tests

Run `npm ci`, `npm run check`, and `npm run test:integration` from this directory. The integration tests require `DEX_FLOW_SERVICE_ADDRESS` for a running `dexcli dev` server; the meeting test binds its Google stand-in to an OS-assigned port, and the event-changed tests drive the example application against an in-process sync feed. Live Google authorization, delegated JWT minting against Google, Calendar API responses, incremental sync against a live calendar, and Dex Web Connections were not verified.
