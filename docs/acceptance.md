# Connector library acceptance

## Automated checks

Run the repository-wide shared and catalog checks:

```bash
make test-common catalog-check
```

Run every registered connector, Studio bundle, and React package:

```bash
make check
```

Each connector README owns its exact unit, vet, integration, live, example, and
UI verification commands. Connector modules always run with `GOWORK=off`; a
local workspace is optional and generated with `make workspace`.

The automated suite must prove these cross-connector contracts:

- `catalog.yaml` is sorted, complete, safe, and contains each connector once;
- every discovered manifest is registered and every registered directory has
  a valid manifest, module path, matching company directory, and company logo;
- public catalog expansion preserves the published schema, ordering, and
  byte-for-byte determinism;
- every operation has exactly one non-optional branch without naming any
  connector, operation, branch, or timeout in shared tests;
- generated Config, Credentials, definitions, factories, Triggers, branches,
  Attributes, Streams, and defaults match each connector manifest;
- generated code is current and standalone modules compile without a workspace;
- release planning validates semantic-version transitions and produces only
  pending connector releases by default;
- UI and release artifacts are deterministic, checksummed, credential-safe,
  and reject active or unsafe content;
- secrets and provider payloads never enter durable state, Results, receipts,
  Streams, logs, generated values, catalogs, or release artifacts.

## Change selection and sharding

Inspect all connectors or one changed range with the same selector used by CI:

```bash
go run ./cmd/connectorctl test-matrix --catalog catalog.yaml --scope all
go run ./cmd/connectorctl test-matrix --catalog catalog.yaml --scope changed \
  --base origin/main --head HEAD
```

Selector tests cover connector-local edits, catalog additions and removals,
shared SDK or schema edits, documentation-only edits, nested connector paths,
and fail-safe unknown shared source changes. Shard tests cover 1, 256, and more
than 256 connectors, proving deterministic assignment without duplication or
omission.

CI behavior is accepted when:

- pull requests and ordinary pushes test only affected connector shards;
- shared contract changes select the complete catalog;
- documentation, agent-rule, and directory-site-only changes run shared checks
  without connector module jobs;
- scheduled compatibility selects the complete catalog;
- no matrix creates more than 256 jobs and Connector jobs cap parallelism at 32.

## Dex CLI and Web compatibility

Current compatibility uses the Dex release pinned in `.dex-compat-version` and
accepts an explicit connector-directory set:

```bash
make test-dex-compat-current
```

It builds selected connectors through a temporary file-backed Go module proxy,
runs schema 2.0 visualization twice in clean consumer modules, and checks valid,
deterministic output with complete identities. It also builds real release
metadata and optional Studio UI artifacts, then runs Dex Web's catalog,
checksum, caching, sandbox, OAuth, and API-key security suites.

Released compatibility downloads the selected public component tags and their
unmodified release artifacts:

```bash
make test-dex-compat-released
```

The scheduled canary uses `DEX_CLI_VERSION=latest` and the complete catalog.
Logs identify the selected Dex tag, connector tag, source commit, and artifact
digest. Exact tags can be supplied for failure reproduction.

## Real Dex integration

With the pinned Dex CLI development server running, execute:

```bash
dexcli dev
make test-integration
```

Shared integration tests cover behavior that crosses real Worker, Client,
persistence, retry, Trigger, RPC, Attribute, Stream, or transition boundaries.
Connector-local integration tests own provider-specific branches, execution
defaults, delivery ordering, recovery, and example behavior. Unit tests do not
simulate those boundaries merely to duplicate an integration assertion.

## Release acceptance

Normal `main` pushes produce a matrix containing only connectors whose manifest
version requests the next patch, minor, or major release. Each connector job
runs current compatibility before publication and released compatibility after
publication. More than 256 pending releases fails with guidance to split work
by independently released module.

A workflow rerun keeps its original matrix. Manual repair accepts one optional,
catalog-validated connector directory; an empty value rebuilds only the public
catalog and site. Pages deployment occurs only after every requested release
succeeds or when no connector release is requested.

The public `ConnectorCatalog`, `dist/pages/catalog.yaml`, URLs, search, and
connector detail wire format remain unchanged.

## Documentation and UI/UX

Public contract changes update the owning root contract page. Every connector
README owns provider architecture, security boundaries, runnable examples, and
verification. Documentation snippets come from runnable files in that
connector's `examples/` tree.

React primitives and Connector Studio behavior are unchanged. Directory-site
virtualization and pagination for very large catalogs are intentionally outside
this acceptance scope.
