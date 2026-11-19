# Supabase sample Flow

The runnable Flow in [flow/workflow.go](flow/workflow.go) calls `getRow` through the static `supabase` Dex Web connection and completes with its typed result. An unwired optional provider branch fails the Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-supabase/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-supabase
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/supabase-release
go run ./cmd/connectorctl release-artifact --manifest connectors/supabase/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/supabase/go --version v0.3.0 --tag connectors/supabase/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/supabase-release/connector-release.json --digest-output /tmp/supabase-release/connector-release.json.sha256
dexcli dev -open=false -dex-port 9590 -web-port 9591 --flow-rendering-dir "$PWD/connectors/supabase/go/build" --connector-release-override supabase=/tmp/supabase-release
```

In Dex Web Connections, create `supabase / supabase`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/supabase/go` using `go run ./examples/sample-supabase`. In Dex Web Start Flow, select `SupabaseSample` and the Worker, and enter an input matching `GetRowInput`. Use a real table, filter column, and row value:

```json
{"table":"projects","column":"id","value":"1"}
```

The mock-backed real-Dex integration test runs in CI.
