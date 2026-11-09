# Campaign Monitor client discovery Flow

This runnable Flow calls `listClients` with the static `campaignmonitor` Dex Web connection. It stores client IDs and names in an Attribute and completes with the typed result. Optional branches are unwired and fail the Flow when selected. The API key does not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/campaignmonitor/go`, run:

```bash
dexcli visualize ./examples/client-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/campaignmonitor-client-discovery-fdg
```

The command must report `valid: true` with no diagnostics.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override campaignmonitor=<artifact-directory>`. In Dex Web Connections select `campaignmonitor / campaignmonitor`. Sign in at [Campaign Monitor](https://login.createsend.com/l) and open Profile image > Account settings > API keys ([guide](https://help.campaignmonitor.com/articles/Knowledge/api-keys)), and save the full account key in secret `api_key`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/campaignmonitor/go`:

```bash
GOWORK=off go run ./examples/client-discovery
```

Start `CampaignMonitorClientDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/client-discovery/flow -count=1`. Live provider authorization remains unverified.
