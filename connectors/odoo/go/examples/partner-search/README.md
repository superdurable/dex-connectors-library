# Odoo partner search Flow

This runnable Flow calls `searchPartners` through the static `odoo` connection, persists bounded partner IDs and names in an Attribute, and exposes the result through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/odoo/go`, run:

```bash
dexcli visualize ./examples/partner-search/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/odoo-partner-search-fdg --open=false
```

An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override odoo=<artifact-directory>`. In Dex Web Connections select the `odoo` connection. Follow the [connection guide](../../../README.md) to enter `baseUrl`, `database`, and secret `apiKey`; leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/odoo/go`:

```bash
GOWORK=off go run ./examples/partner-search
```

Start `OdooPartnerSearch` with a unique Flow ID and input `{"namePattern":"%Acme%","limit":5}`. Replace the sample with an Odoo `ilike` name pattern and a bound of 1 to 100. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Live Odoo authorization and search remain unverified.
