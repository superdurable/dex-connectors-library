# Supabase sample Flow

The runnable Flow in [flow/workflow.go](flow/workflow.go) calls `selectRows` through the static `supabase` Dex Web connection and completes with its typed result. An unwired optional provider branch fails the Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-supabase/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-supabase
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/supabase-release
go run ./cmd/connectorctl release-artifact --manifest connectors/supabase/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/supabase/go --version v0.2.0 --tag connectors/supabase/go/v0.2.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/supabase-release/connector-release.json --digest-output /tmp/supabase-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/supabase/go/build" --connector-release-override supabase=/tmp/supabase-release
```

In Dex Web Connections, create `supabase / supabase`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/supabase` using `go run ./examples/sample-supabase`. In Dex Web Start Flow, select `SupabaseSample` and the Worker, and enter an input matching `SelectRowsInput`. For a start input, use a real ID from your provider in place of each sample ID:

```json
{"table":"projects","limit":10}
```

The mock-backed real-Dex integration test runs in CI.
