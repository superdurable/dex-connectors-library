# SendGrid template-list Flow

This runnable Flow calls `listTemplates` with the static `sendgrid` connection. Typed start input passes to the generated operation Step, and the template page completes the Flow. Dex Web summary and display RPCs show the persisted result. Optional provider branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/sendgrid`, run:

```bash
mkdir -p build
dexcli visualize ./examples/template-list/flow/workflow.go --schema-version 2.0 --json --out ./build/template-list
```

The command must report `valid: true`. A local unpublished module may report `connector_release_required` until its release metadata is supplied.

## Run with Dex Web

Create the connector release artifact from this source, pass it to `dexcli dev` with `--connector-release-override sendgrid=<artifact-directory>`, and use the generated `build` Flow rendering. Open Dex Web Connections, select `sendgrid / sendgrid`, and follow the key guide at https://app.sendgrid.com/settings/api_keys. Leave the base URL and safety limits blank for defaults; use `https://api.eu.sendgrid.com` for an EU regional subuser.

From `connectors/sendgrid`, with the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/template-list
```

In Dex Web Start Flow, choose `SendGridTemplateListDemo` and enter a unique Flow ID with this input:

```json
{"generation":"dynamic","pageSize":100}
```

The Flow returns `TemplatesOutput`. Leave `pageToken` blank for the first page; pass the preceding result's `nextPageToken` for another page. `pageSize` must be 1–200, and zero selects 100. The pull Worker connects to Dex without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test -race ./...` from the connector directory.
