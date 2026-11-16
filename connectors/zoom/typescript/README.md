# Zoom TypeScript connector

`@superdurable/connector-zoom` 0.2.0 implements the five Zoom Meetings operations from the Go connector: `listMeetings`, `getMeeting`, `createMeeting`, `updateMeeting`, and `listPastMeetingParticipants`, and the `meetingChanged` poll Trigger. Generated models, operation-specific Step factories, connection bindings, mock cases, and OAuth declarations come from `../connector.yaml`. Provider requests and operation invocation are hand-written in `src/client.ts`, `src/meeting.ts`, and `src/operations.ts`; the Trigger is in `src/trigger.ts`, `src/meeting_list_fetcher.ts`, and `src/meeting_snapshot.ts`.

Install the package with `@superdurable/connectors-sdk` 0.12.0, which adds the poll Trigger kit, and Dex TypeScript SDK 1.45.4 or later. Use Node.js 22 or 24. Keep Zoom credentials in Dex Web Connections. `newProjectConnection(project, ConnectionName)` binds generated private and shared credential decoders and durable OAuth refresh. It does not accept tokens in Flow inputs.

## meetingChanged Trigger

`meetingChanged` delivers each meeting the authorized user schedules, changes, or that leaves Zoom's scheduled list, as a `MeetingChangedEvent` with `changeKind` `scheduled`, `updated` (with `previousMeeting`), or `removed` (with the last listed state). Each poll reads every page of `GET /users/me/meetings?type=scheduled` and diffs it with a keyed snapshot committed in the binding's poll cursor. Behavior matches the Go connector, which the [connector README](../README.md#meetingchanged-trigger) describes in full: the first poll records a baseline and delivers nothing, event IDs are stable across refetches, a recurring meeting tracks its next listed occurrence, an expired page token restarts the listing once, more than 100 changes commit 100 at a time, a cursor that is undecodable or holds another Zoom user's meetings reseeds with a WARN record, and every 429, including the daily limit, pauses the connection for `Retry-After`. The cursor token and snapshot entries are byte-compatible with Go, and inbox payloads use the Go JSON fields, so Go and TypeScript replicas can share one binding; `test/meeting_changed_go_parity.test.ts` replays a Go recording to prove it.

The connection's `pollInterval` setting, `Config.pollIntervalMs` in code, is the delay between polls; `ZoomClient` rejects one under 30 seconds. The Trigger reads with the `meeting:read:list_meetings` scope the connection already requests.

| Function | Use |
| --- | --- |
| `newProjectMeetingTriggerRunner(project, connection, routes)` | Durable delivery from a `LoadedProject`: each route's saved binding configuration is loaded (an unsaved binding delivers every kind), and each change is recorded in the binding's project Trigger inbox before its snapshot commits. |
| `newMeetingTriggerRunner(connection, routes)` | One poll `Runner` for `MeetingChangedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `newMeetingChangedTrigger({ connection, bindingName, target })` | One in-memory `TriggerRunner`, like Go's generated `NewMeetingChangedTrigger`; it records a new baseline on every start. |
| `defineMeetingChangedTriggerBinding({ connectionName, bindingName })` | Declares the binding in a Flow's `getConnectorTriggerBindings`. Dex Web shows no field for it. |

`MeetingChangedTriggerConfiguration.changeKinds` narrows deliveries in code; skipped kinds still advance the snapshot. The [meeting-changed example](examples/meeting-changed/README.md) wires a Flow target in [`app.ts`](examples/meeting-changed/app.ts):

```ts
const runner = newProjectMeetingTriggerRunner(project, connection, [{ bindingName: MeetingChangedTriggerBinding, target: newMeetingChangeFlowTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
```

`runner.run(newTriggerContext(signal))` replays the inboxes, seeds, and polls until the signal aborts, then rejects with `TriggerDeliveryAbortedError`.

## Run the examples

The [meeting-list example](examples/meeting-list/) has the static `zoom-scheduler` connection name, a Worker entrypoint, a Client starter, and a real Dex integration check. Create the user-managed Zoom OAuth app and authorize it using the guide in the [connector README](../README.md). Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its server address, then run these commands from this directory:

```sh
npm ci
npm run example:meeting-list
npm run example:meeting-list:start -- examples/meeting-list/input.example.json
```

The integration check uses a generated provider stand-in and needs no Zoom credentials:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run example:meeting-list:integration
```

The [meeting-changed example](examples/meeting-changed/README.md) starts one Flow per meeting change on `zoom-meeting-watcher`; run it with `npm run example:meeting-changed`.

Run `npm run check` for package tests, types, and lint. With `dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration` runs both examples' real Dex integration tests. From the repository root, run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/zoom` to replay the Go-recorded provider scenarios in TypeScript. Live Zoom authorization, polling, and provider behavior have not been verified.
