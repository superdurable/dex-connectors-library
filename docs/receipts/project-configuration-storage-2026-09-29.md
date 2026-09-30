# Project configuration storage verification — 2026-09-29

## Source and scope

Official repository: `git@github.com:superdurable/dex-connectors-library.git`.
Worktree: `/private/tmp/sv2-connectors-project-config`; branch
`codex/v2-project-configuration`; starting commit
`f40af0cfc996c6d2312f356329d05079b6d5faff`.

The changes are local source work, not a publication. Connector module pins,
released providers, and V1 deployments remain unchanged. The existing Connector
SDK pins Dex Go SDK `v0.11.3`; it was retained. The isolated integration used Dex
Server and CLI `v1.1.2`.

Added `sdkgo/projectconfig` with shared S3/MinIO conditional versioned storage,
configuration snapshots, credential exchange fencing, immutable-result recovery,
and default-chain environment loading. The typed Connector SDK adapter is in
`projectconfig/provider`; the shared core's dependency closure contains no Dex
Worker SDK, protobuf, or static connector modules. This split was required by a
real Dex Web test: importing the Worker SDK into the Server process caused a
duplicate Dex protobuf registry panic. The conflict was removed by dependency
boundaries, not suppressed.

Added optional generic Trigger configuration to the manifest schema, string-list
item enum and uniqueness validation, generated typed validation, and Stripe's
`checkoutSessionUpdated.eventTypes` declaration. Stripe metadata remains
`v0.2.2` to defer release. The new source metadata is **not** present in the
already published `v0.2.2` artifact and needs a separately authorized release.

## Checks

- `make test-common`: passed SDK/root Go race suites, vet, and 88 Python policy
  tests. New coverage includes concurrent independent clients, expiry-only use,
  unknown admission writes, abandoned admissions, immutable result recovery,
  replacement/revocation fencing, configuration version freezing, redaction,
  scope/endpoint validation, and S3 conditional/encryption/version headers.
- `GOWORK=off go test -race ./...` and `go vet ./...` in `connectors/stripe`:
  passed, including examples and signed webhook/provider tests.
- `go run ./cmd/connectorctl validate connectors/stripe/connector.yaml` and
  `generate --check`: passed. Schema and generator regressions passed.
- `go list -deps ./projectconfig`: the only Superdurable import was the package
  itself; no protobuf package was present.
- JSON Schema document parses as JSON. A separate Python JSON Schema validator
  was unavailable; Go semantic manifest/schema tests were executed.

## Real versioned S3 and executable example

`make test-projectconfig-integration` passed against the owned Kind MinIO at
`http://127.0.0.1:29000`. Fixture credentials were read privately from the existing
host fixture environment and supplied in the child process environment; no
credential values were printed or committed.

Final fixture bucket: `sv2-projectconfig-1790730706546300000`.
Eight independently constructed S3 clients and ConnectionStores concurrently
used one expired credential. Exactly one provider refresh was dispatched.
Conditional stale ETag writes failed; exact historical version
`bba1bf65-5ec1-492c-859c-f74ebf91e719` remained readable. A simulated process loss
after immutable result persistence, before the head update, recovered READY
revision 5 through another client without repeating provider exchange.

The same test froze ordinary configuration, loaded it through the complete
`DEX_PROJECT_*` startup environment, and ran
`go run ./examples/projectconfiguration` successfully against the exact version
and digest. Cross-project loading was rejected. Test cleanup removed only the
unique fixture bucket and all of its object versions.

MinIO emitted one AWS SDK warning that the response lacked its optional
transport checksum. Configuration and credential object references were still
verified using the protocol's exact version and SHA-256 digest checks.

## Real Dex credential boundary

Command:

```sh
make test-projectconfig-dex-integration
```

Environment selected the owned Dex endpoint `127.0.0.1:28811`, advertised the
scoped test Worker through `host.docker.internal`, and selected the same local
MinIO fixture. The existing `integrationtest_test.connectorConsumerFlow` was
reused; no production Flow schema, Step identities, or behavior changed.

- FlowID: `sdk-project-configuration-1790730681493169000`
- RequestID: identical to FlowID, assigned once for this test invocation.
- RunID: `01a0efdd-bcad-73ad-bb50-23288ff7c4f7`
- Result: COMPLETED; credential revision 3; exactly one refresh.
- The `CreateWidget` operation retried once, retained its operation identity,
  and reused the already rotated credential.
- Temporary S3 bucket and test Worker were cleaned; only this completed test
  history remains under normal engine retention.

Read-only verification used pinned `dexcli 1.1.2`:

```sh
dexcli flow summary sdk-project-configuration-1790730681493169000 \
  --server 127.0.0.1:28811 --run-id 01a0efdd-bcad-73ad-bb50-23288ff7c4f7 --no-hydrate
dexcli flow state sdk-project-configuration-1790730681493169000 \
  --server 127.0.0.1:28811 --run-id 01a0efdd-bcad-73ad-bb50-23288ff7c4f7 --no-hydrate
dexcli flow history sdk-project-configuration-1790730681493169000 \
  --server 127.0.0.1:28811 --run-id 01a0efdd-bcad-73ad-bb50-23288ff7c4f7 --all --no-hydrate
```

All returned successfully. Summary reported COMPLETED. State contained the
business input and safe widget result reference. History contained seven
semantic events; `CreateWidget` reported final attempt 2 and the Flow closed
successfully. Credentials were resolved at the operation boundary and did not
enter the Flow input, Attributes, or output.

## Independent review follow-up

Dex Web integration review identified an expired abandoned OAuth admission that
application resolution previously left pending. Actual credential use now
reconciles its immutable result or fences it to reauthorization, without
provider dispatch. Restart tests cover both recovered-result and absent-result
cases. MinIO's definitive `OperationAborted` conditional conflict now maps to
`ErrConflict`. The focused race suite and vet passed after both fixes.

`ExchangeAdmission.ProviderDispatchAllowed` is excluded from JSON and YAML.
A serialized/reloaded admission cannot accidentally retain provider dispatch
permission; a fresh winning conditional admission is required.

## Remaining acceptance

This receipt verifies the shared protocol, SDK adapter, source manifest
extension, and local Dex/S3 integration using explicit provider fixtures. It
does not claim real provider OAuth consent/rotation, hosted KMS/IAM validation,
Dex Web's complete browser journey, or Superverse's three complete Local Kind
business cases. Cross-repository release publication was not performed.
