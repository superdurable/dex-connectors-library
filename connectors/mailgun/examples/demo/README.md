# Mailgun demo Flow

This runnable Flow calls `getEvents` with the static `mailgun` connection. The start input passes through to the generated operation Step; its happy-path result completes the Flow. Dex Web summary and display RPCs show the persisted provider result. Optional provider branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/mailgun`, run:

```bash
mkdir -p build
dexcli visualize ./examples/demo/flow/workflow.go --schema-version 2.0 --json --out ./build/demo
```

The command must report `valid: true`. A local unpublished module may report `connector_release_required` until its release metadata is supplied.

## Run with Dex Web

Create the connector release artifact from this source, pass it to `dexcli dev` with `--connector-release-override mailgun=<artifact-directory>`, and use the generated `build` Flow rendering. Open Dex Web Connections, select `mailgun / mailgun`, and follow the key guide at https://app.mailgun.com/settings/api_security. Leave the base URL and safety limits blank for defaults.

From `connectors/mailgun`, with the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/demo
```

In Dex Web Start Flow, choose `MailgunDemo` and enter a unique Flow ID with this input:

```json
{"domain":"example.test","limit":20}
```

The pull Worker connects to the Dex Server without an inbound listener. The Flow returns `EventsOutput`. The provider connection name is fixed in `flow/workflow.go`.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test -race ./...` from the connector directory.
