# GitHub repository releases example

## Release page Flow

This standalone Worker registers `repositoryreleases.RepositoryReleasesFlow`.
Render `flow/releases_flow.go` with strict FDG 2.0,
then start it through the host's authenticated Start Flow UI with input:

```json
{"owner":"superdurable","repository":"dex","pageSize":10,"page":1,"maxBodyCharacters":4000}
```

The Flow reads one actual GitHub page, exposes the immutable query result in
Summary/Display and the typed `GetReleasePage` RPC, then completes. A nonzero
`nextPage` is an explicit continuation for another page request; this example
does not claim complete repository history or implement a business date filter.

### Flow contract

- Identity: independent top-level `repositoryreleases.RepositoryReleasesFlow`;
  use `github-releases-<UUIDv4>`, for example
  `github-releases-550e8400-e29b-41d4-a716-446655440000`. Each explicit page read
  has a new business UUID. The host starts it with a complete actor/FlowID/input
  RequestID, `IDReuseDisallow`, and same-request `IgnoreError`. A lost response
  is reconciled against that original identity; a retry does not create a new
  FlowID. No SubFlow or Continue-As-New.
- Attribute: ordinary `github-release-page`, typed `ListReleasesResult`, owns
  the single bounded query result. No indexes, concurrent writers or locks.
  AttributeMaps, Channels, ChannelMaps and Streams: **None**.
- Graph: `ListRepositoryReleases → RecordReleasePage → complete`. The Connector
  executes one GET with its manifest's 30-second timeout and bounded query retry
  policy. Optional error branches fail the Flow if unwired. RecordReleasePage
  writes once, with a 10-second timeout and one attempt. No mutation or provider
  cleanup is required; cancellation follows the provider request context.
- RPCs: `GetReleasePage`, `GetDexSummary`, `GetDexDisplay` are read-only and
  expose no credentials. The management host enforces viewer/start authorization.
  Results remain readable after completion for the configured Dex retention.
  There is no archive state or new store. The prior repository-changes Flow and
  all of its primitive and Step identities remain unchanged.
- Validation: strict FDG, module build/vet, and an actual Dex/Worker/GitHub run
  must be recorded separately from legacy scripted-provider example tests.

## Run and configure

From `connectors/github`, run `GOWORK=off go run ./examples/repository-releases`.
The Worker reads the `DEX_PROJECT_*` project configuration environment
described in [project configuration](../../../../sdkgo/projectconfig/README.md),
which Dex Web or Superverse Studio writes, and the standard
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS` and `DEX_BLOB_CACHE_DIR`
settings documented by the [repository changes setup](../repository-changes/README.md).
Use a distinct local Worker address when both examples run.

The logical connection is `github-repository-releases`. Configure the released
GitHub v0.21.0 module through the host Configuration UI and complete OAuth with
exactly the documented profile/email scopes; no additional repository scope is
requested. The existing authorization guide, provider defaults and shared
connection configuration apply unchanged. `listReleases` adds no operation
settings or new form fields. Credentials remain in the private configuration
store and are resolved by the official adapter on each actual call.

For a strict definition, copy this Flow into a clean consumer module requiring
`github.com/superdurable/dex-connectors-library/connectors/github@v0.21.0`, run
`go mod tidy`, then:

```sh
dexcli visualize ./flow/releases_flow.go --schema-version 2.0 --json --out ./build/releases
```

The repository compatibility check makes an isolated exact-version consumer and
requires deterministic valid output. Source builds and definition checks do not
prove OAuth or provider acceptance. Record real Dex/GitHub results separately.
