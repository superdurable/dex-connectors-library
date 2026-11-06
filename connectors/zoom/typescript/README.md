# Zoom TypeScript connector

`@superdurable/connector-zoom` implements the five Zoom Meetings operations from the Go connector: `listMeetings`, `getMeeting`, `createMeeting`, `updateMeeting`, and `listPastMeetingParticipants`. Generated models, operation-specific Step factories, connection bindings, mock cases, and OAuth declarations come from `../connector.yaml`. Provider requests and operation invocation are hand-written in `src/client.ts`, `src/meeting.ts`, and `src/operations.ts`.

Install the package with `@superdurable/connectors-sdk` 0.6.0 and Dex TypeScript SDK 1.28.0 or later. Use Node.js 22 or 24. Keep Zoom credentials in Dex Web Connections. `newProjectConnection(project, ConnectionName)` binds generated private and shared credential decoders and durable OAuth refresh. It does not accept tokens in Flow inputs.

## Run the example

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

Run `npm run check` for package tests, types, and lint. From the repository root, run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/zoom` to replay the Go-recorded provider scenarios in TypeScript. Live Zoom authorization and provider behavior have not been verified.
