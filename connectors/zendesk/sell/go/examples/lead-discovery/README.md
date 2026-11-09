# Zendesk Sell lead discovery Flow

This runnable Flow calls `listLeads` with the static `zendesk-sell` Dex Web connection. It stores a bounded lead page in an Attribute and completes with the typed result. Start input `{"perPage":20,"page":1}` reads the first page; increase `page` to read another page. Optional branches are unwired and fail the Flow when selected. Credentials do not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/zendesk/sell/go`, run:

```bash
dexcli visualize ./examples/lead-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/sell-lead-discovery-fdg
```

The command must report `valid: true`. Before the first release, `connector_release_required` may warn that automatic Dex Web configuration requires a published release.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override zendesk-sell=<artifact-directory>`. In Dex Web Connections select `zendesk-sell / zendesk-sell`. Sign in to [Zendesk Sell](https://app.futuresimple.com/) and open Integrations > OAuth > Access Tokens. Click Add User Token, save a description, and copy the one-time secret to Dex `access_token`. Leave `baseUrl` blank to use the fixed provider API root.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/zendesk/sell/go`:

```bash
GOWORK=off go run ./examples/lead-discovery
```

Start `ZendeskSellLeadDiscovery` with a unique Flow ID and input `{"perPage":20,"page":1}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/lead-discovery/flow -count=1`. Live provider authorization remains unverified.
