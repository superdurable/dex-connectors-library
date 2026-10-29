# Mixpanel event track example

This runnable Flow imports one event from Dex Web Start Flow. The start Step fixes a missing `time` once before dispatch; the connector derives `$insert_id` from Dex's stable call identity. The `imported` branch completes after Mixpanel confirms strict ingestion. Optional branches are unwired and fail the Flow. `ConnectionName` is statically `mixpanel`. Dex Web summary and display views show the completed provider outcome.

## Generate strict FDG 2.0

From `connectors/mixpanel` with the latest stable Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/event-track/flow/workflow.go --schema-version 2.0 --json --out ./build/event-track
```

The result must report `valid: true`. Local source may report `connector_release_required` until published.

## Run

From the repository root, create a release override and run Dex:

```bash
mkdir -p /tmp/mixpanel-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/mixpanel/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/mixpanel \
  --version v0.1.0 --tag connectors/mixpanel/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/mixpanel-release/connector-release.json \
  --digest-output /tmp/mixpanel-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/mixpanel/build" --connector-release-override mixpanel=/tmp/mixpanel-release
```

Set the project environment described by [`projectconfig`](../../../../sdkgo/projectconfig/README.md), then start the Worker:

```bash
cd connectors/mixpanel
go run ./examples/event-track
```

Configure `mixpanel / mixpanel` in Dex Web Connections using a service account created in [Organization Settings > Service Accounts](https://mixpanel.com/settings/org/) and your numeric ID from [Project Settings > Overview](https://mixpanel.com/settings/project/). In Start Flow choose `MixpanelEventTrack`, select the Worker, enter a unique Flow ID, and submit:

```json
{"event":"Signed Up","distinctID":"member-1"}
```

The start Step records the current Unix-second `time` once. You may supply an earlier valid timestamp in seconds. A retry reuses the same time and distinct ID.

## Tests

`GOWORK=off go test ./...` checks deterministic timestamp preparation. CI runs the generated-mock real-Dex test. Live Mixpanel behavior remains unverified.
