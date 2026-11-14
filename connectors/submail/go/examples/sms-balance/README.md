# SUBMAIL SMS Balance Flow

This runnable Flow calls `getSMSBalance` through the static `submail` connection, persists the returned credit balances, and completes with a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/submail/go`, run:

```bash
dexcli visualize ./examples/sms-balance/flow/workflow.go --schema-version 2.0 --json --out /tmp/submail-sms-balance-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override submail=<artifact-directory>`. In Dex Web Connections select `submail`, enter `appId` and secret `appKey` according to the [connection guide](../../../README.md), and leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/submail/go`:

```bash
GOWORK=off go run ./examples/sms-balance
```

Start `SUBMAILSMSBalance` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. The real-Dex integration test runs with a mock connection. Live provider authorization and balances remain unverified.
