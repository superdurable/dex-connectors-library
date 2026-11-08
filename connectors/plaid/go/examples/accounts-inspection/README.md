# Plaid accounts inspection Flow

This runnable Flow calls `getAccounts` through the static `plaid` connection. It persists the selected account routing summaries in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/plaid/go`, run:

```bash
dexcli visualize ./examples/accounts-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/plaid-accounts-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override plaid=<artifact-directory>`. In Dex Web Connections select the `plaid` connection. Follow the [connection guide](../../../README.md) to enter `client_id`, `secret`, and the linked Item `access_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Sandbox defaults; select the Production API URL for Production.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/plaid/go`:

```bash
GOWORK=off go run ./examples/accounts-inspection
```

Start `PlaidAccountsInspection` with a unique Flow ID and input `{"itemId":"item_123456"}`. Replace the sample with a Item ID returned by Plaid. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Plaid authorization and retrieval remain unverified.
