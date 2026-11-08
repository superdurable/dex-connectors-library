# Drip account discovery Flow

This runnable Flow calls `listAccounts` with the static `drip` Dex Web connection. It stores account IDs and names in an Attribute and completes with the typed result. Optional branches are unwired and fail the Flow when selected. The API key does not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/drip/go`, run:

```bash
dexcli visualize ./examples/account-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/drip-account-discovery-fdg
```

The command must report `valid: true` with no diagnostics.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override drip=<artifact-directory>`. In Dex Web Connections select `drip / drip`. Open [Drip user profile](https://www.getdrip.com/user/edit), and save the personal API Token in secret `api_key`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/drip/go`:

```bash
GOWORK=off go run ./examples/account-discovery
```

Start `DripAccountDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/account-discovery/flow -count=1`. Live provider authorization remains unverified.
