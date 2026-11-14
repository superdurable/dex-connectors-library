# Udesk ticket status list Flow

This runnable Flow uses the static `udesk` connection to list account ticket statuses. The `listed` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/udesk/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/ticket-status-list/flow/workflow.go --schema-version 2.0 --json --out ./build/ticket-status-list
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override udesk=<artifact-directory>`. In Dex Web **Connections**, create the `udesk` connection with your `https://<subdomain>.udesk.cn` account origin, super administrator email, and an `open_api_auth_token` from [Udesk Open API v2](https://www.udesk.cn/doc/apiv2/intro/). Leave timeout and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/ticket-status-list
```

In Dex Web **Start Flow**, choose `UdeskTicketStatusList`, enter a unique Flow ID, and use `{}` for all statuses or `{"enabledOnly":true}` to filter. The output contains `statuses`, which can be empty. The mock-backed integration test uses a real Dex Worker and Client without Udesk credentials.
