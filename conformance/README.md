# Cross-language connector wire fixtures

These JSON files are generated from sdkgo types. Python and TypeScript ports should decode and re-encode each fixture, comparing JSON values and preserving the field names, nulls, and omissions shown here. All credential-looking values are inert `REDACTED_*` placeholders.

Run `cd sdkgo && UPDATE_CONFORMANCE_FIXTURES=1 GOWORK=off go test ./... -run '^TestConformance' -count=1` to regenerate. `make test-common` checks committed fixtures for drift.

| Fixture | sdkgo source of truth |
| --- | --- |
| `results.json` | `sdkgo/types.go:167` (`QueryResult`), `sdkgo/types.go:179` (`MutationResult`), `sdkgo/types.go:191` (`Receipt`) |
| `failures.json` | `sdkgo/errors.go:14` (every `FailureKind`), `sdkgo/errors.go:46` (`Failure` fields) |
| `identity.json` | `sdkgo/types.go:25` (`CallID` validation), `sdkgo/types.go:151` (`Call`), `sdkgo/types.go:492` (length-prefixed SHA-1 UUID derivation), `sdkgo/types.go:353` (key fallback) |
| `retry-policy.json` | `sdkgo/retry_policy.go:20` (default Dex retry policy) |
| `single-dispatch-markers.json` | `sdkgo/singledispatch/fence.go:20` (states), `sdkgo/singledispatch/fence.go:30` (`Marker`) |
| `connections.json` | `sdkgo/projectconfig/dexweb.go:20` (schema), `sdkgo/projectconfig/dexweb.go:38` (local connection), `sdkgo/projectconfig/dexweb.go:50` (file) |
| `project-environment.json` | `sdkgo/projectconfig/environment.go:44` (Dex Web selector), `sdkgo/projectconfig/environment.go:53` (`DEX_PROJECT_*` loader) |
| `project-storage.json` | `sdkgo/projectconfig/storage.go:25` (object version and ETag), `sdkgo/projectconfig/storage.go:77` (scope prefix), `sdkgo/projectconfig/connections.go:326` (connection key), `sdkgo/projectconfig/configuration.go:101` (snapshot), `sdkgo/projectconfig/trigger_inbox.go:85` (inbox key) |
| `credential-broker.json` | `sdkgo/projectconfig/credential_broker.go:47` (status), `sdkgo/projectconfig/credential_broker.go:53` (request), `sdkgo/projectconfig/credential_broker.go:59` (response) |
| `connectormock.json` | `sdkgo/types.go:113` (definitions), `sdkgo/connectormock/call.go:40` (recorded calls), `sdkgo/connectormock/report.go:18` (report location), `sdkgo/connectormock/report.go:30` (report row) |

`MutationAttempt` and `QueryAttempt` select a terminal branch or request a Dex retry; they are private in-memory decisions (`sdkgo/errors.go:104`, `sdkgo/errors.go:139`). Only their validated `QueryResult` and `MutationResult` envelopes cross the JSON boundary. A Retry is **never serialized** as a terminal result. It continues under the Step retry policy, and exhaustion fails the Flow. An `uncertain` result is reserved for a potentially accepted non-repeatable write that read-back cannot confirm.

`retry-policy.json` contains the Dex Go SDK's JSON form: duration fields are integer nanoseconds and field names are capitalized. `project-storage.json` projects only `Object` identity and CAS fields because `Object.MarshalJSON` rejects private object contents (`sdkgo/projectconfig/storage.go:45`). `project-environment.json` separates Dex Web, hosted, and local object-store settings; `DEX_CONNECTOR_CONFIG_FILE` must not be combined with hosted project selectors. The `connections.json` samples cover API key, OAuth refresh, and multiple-secret credential shapes without usable credentials.
