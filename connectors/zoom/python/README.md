# Zoom Python connector

`dex-connector-zoom` v0.1.0 implements the five Zoom Meetings operations as Dex Queries and Mutations: `listMeetings`, `getMeeting`, `createMeeting`, `updateMeeting`, and `listPastMeetingParticipants`. It requires `dex-connectors-sdk==0.6.0` and an OAuth user connection. The manifest in [`../connector.yaml`](../connector.yaml) owns the authorization guide, credential fields, scopes, operation definitions, defaults, and generated model contracts. This language package does not change the Go connector's v0.24.0 release.

`Client` resolves credentials for each call. `new_project_connection` binds a named Dex Web connection through generated refresh and shared-credential wiring. Close the connection after its Worker stops. The `createMeeting` Mutation uses the SDK's durable single-dispatch fence because Zoom has no create idempotency key; an unconfirmed dispatch selects `uncertain` and must be reconciled before any new create.

The checked-in [`meeting_list` example](examples/meeting_list/README.md) provides the Worker entrypoint, start command, strict FDG 2.0 source, and a real Dex Worker/Client integration test. It uses the static connection name `zoom-meetings`. Run the package tests with `uv sync --locked && uv run pytest tests`. Provider conformance replays the Go recording in [`scenarios.json`](../../../conformance/connectors/zoom/scenarios.json).

Real Zoom OAuth authorization and provider behavior are unverified. The Go connector README lists the exact live behaviors that still need author testing through Dex Web Connections.
