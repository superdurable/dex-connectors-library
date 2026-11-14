# JPush application tag Flow

This runnable Flow calls `listApplicationTags` through the static `jiguang` connection, persists the returned tag names, and completes with a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/jiguang/go`, run:

```bash
dexcli visualize ./examples/tag-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/jiguang-tag-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override jiguang=<artifact-directory>`. In Dex Web Connections select the `jiguang` connection and follow the [connection guide](../../../README.md) to enter `appKey` and secret `masterSecret`. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/jiguang/go`:

```bash
GOWORK=off go run ./examples/tag-read
```

Start `JiguangTagRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. CI runs the fixture-backed real-Dex integration test. Real JPush authorization and tag discovery remain unverified.
