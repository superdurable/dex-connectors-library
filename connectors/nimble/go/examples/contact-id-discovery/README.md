# Nimble contact ID discovery Flow

This runnable Flow calls `listContactIDs` with the static `nimble` Dex Web connection. It stores a bounded contact ID page in an Attribute and completes with the typed result. Start input `{"page":1,"perPage":20}` reads the first page; increment `page` to read another page. Optional branches are unwired and fail the Flow when selected. Credentials do not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/nimble/go`, run:

```bash
dexcli visualize ./examples/contact-id-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/nimble-contact-id-discovery-fdg
```

The graph must report `valid: true`. Before the first release, `connector_release_required` may warn that automatic Dex Web configuration requires a published release.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override nimble=<artifact-directory>`. In Dex Web Connections select `nimble / nimble`. Sign in at [Nimble CRM](https://app.nimble.com/) as an account administrator, open **Settings > API Token > Generate New Token**, and save the generated token in `api_key`. An administrator may first need to grant API access under **Settings > Users**.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/nimble/go`:

```bash
GOWORK=off go run ./examples/contact-id-discovery
```

Start `NimbleContactIDDiscovery` with a unique Flow ID and input `{"page":1,"perPage":20}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8945`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8945 GOWORK=off go test -race -tags=integration ./examples/contact-id-discovery/flow -count=1`. Live provider authorization remains unverified.
