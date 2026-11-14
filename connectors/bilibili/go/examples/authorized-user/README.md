# Bilibili authorized user Flow

This runnable Flow calls `getAuthorizedUser` through the static `bilibili` connection, persists the typed identity, and completes with it. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/bilibili/go`, run:

```bash
dexcli visualize ./examples/authorized-user/flow/workflow.go --schema-version 2.0 --json --out /tmp/bilibili-authorized-user-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override bilibili=<artifact-directory>`. In Dex Web Connections select `bilibili` and follow the [connection guide](../../../README.md) to enter clientID, secret appSecret, and secret accessToken. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/bilibili/go`:

```bash
GOWORK=off go run ./examples/authorized-user
```

Start `BilibiliAuthorizedUser` with a unique Flow ID and input `{}`. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live Bilibili authorization and account data remain unverified.
