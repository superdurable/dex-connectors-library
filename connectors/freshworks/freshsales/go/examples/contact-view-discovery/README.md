# Freshsales contact-view-discovery Flow

This runnable Flow calls `listContactViews` with the static `freshsales` connection. Its happy-path contact views are saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Only contact view IDs and names enter the Flow result; the API Key does not.

## Strict FDG 2.0 generation

From `connectors/freshworks/freshsales/go`, run:

```bash
dexcli visualize ./examples/contact-view-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/freshsales-contact-view-discovery-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override freshsales=<artifact-directory>`. In Dex Web Connections select `freshsales / freshsales`. Sign in at https://app.freshworks.com/, open your CRM account > profile picture > Profile Settings > API Settings, and copy the Bundle alias as the full `baseUrl` ending in `/crm/sales/api`. Copy Your API key into the secret `api_key` field. Blank `baseUrl` is invalid; blank timeout and response-size fields use their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/freshworks/freshsales/go`:

```bash
GOWORK=off go run ./examples/contact-view-discovery
```

Start `FreshsalesContactViewDiscovery` with a unique Flow ID and input `{}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/contact-view-discovery/flow -count=1`. Live provider authorization remains a separate check.
