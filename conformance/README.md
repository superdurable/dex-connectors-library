# Cross-language connector wire fixtures

These JSON files are generated from sdkgo types and production storage writes. Python and TypeScript ports should decode and re-encode each fixture using **JSON value equality, not bytes**: key order, whitespace, and equivalent string escapes may differ. Field names, array order, nulls, and omissions must match. All credential-looking values are inert `REDACTED_*` placeholders.

Run `cd sdkgo && UPDATE_CONFORMANCE_FIXTURES=1 GOWORK=off go test ./... -run '^TestConformance' -count=1` to regenerate. `make test-common` checks committed fixtures for drift.

| Fixture | sdkgo source of truth |
| --- | --- |
| `results.json` | `sdkgo/types.go:167` (`QueryResult`), `sdkgo/types.go:179` (`MutationResult`), `sdkgo/types.go:191` (`Receipt`) |
| `failures.json` | `sdkgo/errors.go:14` (every `FailureKind`), `sdkgo/errors.go:53` (`Failure` fields) |
| `identity.json` | `sdkgo/types.go:25` (`CallID` validation), `sdkgo/types.go:151` (`Call`), `sdkgo/types.go:492` (length-prefixed SHA-1 UUID derivation), `sdkgo/types.go:353` (key fallback) |
| `retry-policy.json` | `sdkgo/retry_policy.go:20` (default Dex retry policy) |
| `single-dispatch-markers.json` | `sdkgo/singledispatch/fence.go:20` (states), `sdkgo/singledispatch/fence.go:30` (`Marker`) |
| `connections.json` | `sdkgo/projectconfig/dexweb.go:20` (schema), `sdkgo/projectconfig/dexweb.go:38` (local connection), `sdkgo/projectconfig/dexweb.go:50` (file) |
| `project-environment.json` | `sdkgo/projectconfig/environment.go:44` (Dex Web selector), `sdkgo/projectconfig/environment.go:66` (`DEX_PROJECT_*` loader) |
| `project-storage.json` | `sdkgo/projectconfig/storage.go:25` (object version and ETag), `sdkgo/projectconfig/storage.go:77` (scope prefix), `sdkgo/projectconfig/connections.go:326` (connection key), `sdkgo/projectconfig/configuration.go:101` (snapshot), `sdkgo/projectconfig/trigger_inbox.go:85` (inbox key) |
| `credential-broker.json` | `sdkgo/projectconfig/credential_broker.go:47` (status), `sdkgo/projectconfig/credential_broker.go:53` (request), `sdkgo/projectconfig/credential_broker.go:59` (response) |
| `connectormock.json` | `sdkgo/types.go:113` (definitions), `sdkgo/connectormock/call.go:40` (recorded calls), `sdkgo/connectormock/report.go:18` (report location), `sdkgo/connectormock/report.go:30` (report row) |

`MutationAttempt` and `QueryAttempt` select a terminal branch or request a Dex retry; they are private in-memory decisions (`sdkgo/errors.go:149`, `sdkgo/errors.go:114`). Only their validated `QueryResult` and `MutationResult` envelopes cross the JSON boundary. A Retry is **never serialized** as a terminal result. It continues under the Step retry policy, and exhaustion fails the Flow. An `uncertain` result is reserved for a potentially accepted non-repeatable write that read-back cannot confirm.

`retry-policy.json` contains the Dex Go SDK v1.26.0 `dex.RetryPolicy` JSON form: capitalized field names and integer nanosecond durations. In `connectormock.json`, `executeDurability: 2` is the Go SDK's `dex.StepDurabilityAsync` ordinal (`Default=0`, `Sync=1`, `Async=2`). These are **Go implementation JSON**, not a cross-language wire contract; Python and TypeScript should match behavior and time units through their own Dex SDKs. The `reportFile` value, by contrast, is the complete on-disk connector mock report shape, generated through two production appends. `recordedCall` and report row flags are part of the connector mock data contract.

`project-storage.json` includes the documents written by production storage paths and their CAS metadata. The `Object` itself cannot be serialized because it may contain private contents (`sdkgo/projectconfig/storage.go:45`). The local store's `objectCAS.etag` is SHA-256 of the compact connection record bytes followed by `-<version>`; other `ObjectStore` implementations may use opaque ETags. All clients must pass the ETag they read as the exact CAS precondition.

**Bytes used for identifiers and digests.** `identity.json` pins the Call ID namespace and both ASCII and non-ASCII derivations. `deriveCallID` concatenates, in order, flow ID, Step execution ID, connector ID, and operation ID. Each field is UTF-8 encoded and preceded by its **byte length** as a four-byte unsigned big-endian integer. UUIDv5 (SHA-1, RFC 4122 variant) over that payload and the pinned namespace yields the lowercase Call ID. Idempotency keys use that exact Call ID according to the connector's key format.

The credential object key's final component and `objectReference.digest` are lowercase hexadecimal SHA-256 of the exact compact UTF-8 `credentialEnvelopeCanonicalJSON` string. `snapshot.digest` is `sha256:` followed by the same hash of `configurationSnapshotCanonicalJSON`. `connectionRecordCanonicalJSON` pins the local CAS ETag input. These strings are the production `encoding/json.Marshal` bytes before the fixture's pretty printing: fields follow their Go struct declaration order, map keys sort lexicographically, there is no insignificant whitespace, and HTML-sensitive `<`, `>`, `&`, U+2028, and U+2029 are escaped as lowercase `\u` sequences. Preserve required fields and `omitempty` behavior as shown. Ports may use another JSON serializer for ordinary documents, but they must reproduce these pinned canonical strings before deriving those keys or digests.

Timestamps use the Go RFC3339Nano representation: preserve a non-UTC numeric offset, omit the fractional part for whole seconds, and trim trailing fractional zeroes while retaining up to nine digits. A zero `time.Time` field without `omitempty` is `0001-01-01T00:00:00Z`. The fixtures include all four cases.

`project-environment.json` separates Dex Web, hosted, and local object-store settings. Its variable names are shared constants with the production loader; `DEX_CONNECTOR_CONFIG_FILE` must not be combined with hosted project selectors. `credentialBrokerSecretName` is a reserved secret object name, not an environment variable. The broker endpoint is stored in `configurationSnapshotDocument.credentialBroker.endpoint`, not read from an environment variable. The `connections.json` samples cover API key, OAuth refresh, multiple-secret, Unicode, empty-array, and null auth-method-list shapes without usable credentials.
