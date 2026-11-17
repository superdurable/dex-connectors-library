# Copper person discovery Flow

This runnable Flow calls `getPerson` with the static `copper` Dex Web connection. It stores the person's public metadata in an Attribute and completes with the typed result. Optional branches are unwired and fail the Flow when selected. Credentials do not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/copper/go`, run:

```bash
dexcli visualize ./examples/person-lookup/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/copper-person-lookup-fdg
```

The command must report `valid: true`. Before the first release, `connector_release_required` warns that automatic Dex Web configuration requires a published release.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override copper=<artifact-directory>`. In Dex Web Connections select `copper / copper`. Open [Copper](https://app.copper.com/) and navigate to **Settings > Integrations > API Keys**. Generate a key, save it in secret `api_key`, and enter the generating user's email in `user_email`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/copper/go`:

```bash
GOWORK=off go run ./examples/person-lookup
```

Start `CopperPersonLookup` with a unique Flow ID and input `{"personId":"26443553"}`, replacing the sample with a positive person ID from Copper. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/person-lookup/flow -count=1`. Live provider authorization remains unverified.
