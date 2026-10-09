# Connector library architecture

## Repository boundaries

- `catalog.yaml` is the sorted membership allowlist. It contains directories,
  not connector metadata.
- `schema/` owns the strict v1alpha1 manifest model and JSON Schema.
- `internal/codegen/` turns one manifest into connector Go types.
- `sdkgo/` is the independently released Go SDK for identities, factories,
  connections, retries, receipts, Triggers, Attributes, and Streams.
- `sdk/react/` owns credential-free connection-status primitives.
- `connectors/` owns independently released provider modules. Each connector
  owns its manifest, architecture, examples, tests, and optional Studio UI.
- `site/` is the static directory published with the generated public catalog.
- `cmd/connectorctl/` validates and generates connectors, selects CI shards,
  plans releases, and creates release artifacts.

The first directory below `connectors/` is the company, matches
`metadata.company`, and contains `logo.svg`. Every connector is a separate Go
module. Adding one changes only its directory and one `catalog.yaml` entry.

## Catalog expansion

`connector.yaml` is the source of truth for company, release version,
configuration, credentials, definitions, branches, operations, Triggers, UI
metadata, and defaults. The root catalog source uses
`connectors.dex.dev/catalog-source/v1alpha1` and contains only `directories`.

`connectorctl catalog` validates every registered directory and rejects unsafe
paths, symlinks, duplicate IDs, missing or unregistered manifests, company or
logo mismatches, and invalid module paths. Release planning separately rejects
unsupported version transitions. Catalog generation expands the manifests into
the public
`connectors.dex.dev/catalog/v1alpha1` `ConnectorCatalog`. The Pages wire format
does not expose the catalog source schema.

## Dex composition

Applications write no provider-specific Step handler. They own stable Step
types, pure business-to-operation input mapping, required branch targets,
optional Result Attributes, Streams, Execute failure policy, and terminal
behavior. Non-connector Steps remain ordinary application Steps.

The trusted connector-specific `Connection` is constructor-injected when a
Flow registers; its internal `ConnectionRef` is not public start input.
`StepRef[T]` connects one factory to a later factory without constructing that
factory twice. Dex resolves the registered target by stable type and input
type, so the registered target's options apply.

Every operation declares exactly one non-optional happy branch. Other branches
are optional and fail the Flow if selected without a target. Connector-local
tests own branch identities, execution defaults, and provider behavior.

## Factory data flow

```text
STEP_IN
   |
   +-- MapToOperationInput (pure)
   +-- validate Dex identity + operation + connection
   +-- derive UUIDv5 CallID
   +-- Mutation derives IdempotencyKey
   +-- configure application Streams
   +-- CredentialProvider[C].Resolve(Call)
   +-- provider request
   +-- strict Attempt
          +-- Retry --------------------> Go error / Dex policy
          +-- Uncertain Mutation ------> optional uncertain branch
          +-- declared branch ---------> same branch
   +-- set typed Result Attribute
   +-- one typed dex.GoTo
```

Attribute write and branch transition share the Execute commit. Stream writes
are immediate best-effort messages outside that commit. `RunQuery` and
`RunMutation` implement the provider-call portion and remain available to
advanced concrete Steps. RPC is rejected because it lacks a Step execution ID.

## Trigger delivery

```text
provider transport
   |
   +-- TriggerSource matches and decodes the event
   +-- PrepareTriggerDelivery ---------> local inbox fsync
   +-- provider acknowledgement
   +-- durable inbox HandleTrigger
          +-- application wrapper
                 +-- filter -> Flow ID -> StartFlow or InvokeRPC
```

An undeliverable event is removed from the inbox. Other delivery errors remain
retryable with backoff. A Trigger runner replays its inbox in order at startup
and holds the inbox lock for one attempt at a time. Source-specific ordering,
acknowledgement, filtering, and recovery rules belong in the connector README
and real Dex integration tests.

Logs carry identities and error messages, never provider payloads or secrets.
The SDK README defines shared messages and attributes.

## Generated provider surface

Each manifest supplies:

- Go package and field names;
- non-sensitive configuration fields and validation metadata;
- secret or OAuth fields and connection kind;
- authentication methods, whether a connection holds one or several, and each
  method's non-secret configuration fields;
- OAuth2/OIDC protocol metadata when applicable;
- operation authorization, branches, resources, and execution defaults;
- typed operation input and output types;
- Trigger factories and optional Studio metadata.

`connectorctl generate` writes `zz_generated_connector.go`, including strongly
typed configs, operation factories, and the `Operations` seam that a mocked
connection replaces. A manifest with mocks also generates the
`<package>mock` package and its fixture-decoding test. Generated configs embed the canonical
SDK marker and metadata tags used by Dex CLI static analysis. Provider code
contains transport and classification logic; constructor options carry code
dependencies such as HTTP clients and idempotency derivation functions.

The SDK and connectors use independent Go modules and directory-prefixed tags.
An SDK contract change is released first; connectors require that exact
release in later PRs. Git tags, not the source catalog, define published
versions.

## CI and release topology

CI derives connector ownership from the catalog. Local connector changes select
the longest registered directory prefix; shared SDK, schema, code generation,
React, catalog tooling, or test-infrastructure changes select all connectors.
Documentation and directory-site-only changes select none. Unknown shared
source changes select all.

Selected paths are assigned to at most 256 stable SHA-256 buckets, with each
connector in exactly one shard. Standalone module tests always use
`GOWORK=off`. Scheduled compatibility runs select the full catalog. A local
`go.work` is an ignored convenience generated by `make workspace`. The cap
stays within the
[GitHub Actions matrix job limit](https://docs.github.com/en/enterprise-cloud%40latest/actions/reference/limits).

Normal `main` releases include only manifests whose declared versions advance
their reachable tags. Each release job runs current compatibility, publishes
one component, and verifies released compatibility. The generated Pages
catalog and directory site are published only after all requested releases
succeed or when none are pending.

Compatibility jobs resolve the latest stable Dex CLI release and the latest
stable Dex Go SDK on every run, so no checked-in file pins a Dex version. See
[Dex versions](versioning-and-releases.md#dex-versions).

## Studio UI distribution

A manifest may declare a setup entrypoint, Host API range, backend capability
IDs, mock scenarios, and icon. A connector release builds a deterministic
`connector-ui.tgz`; its digest and UI contract are recorded in
`connector-release.json`.

Flow Definition identifies the connector and operation. SuperVerse Studio's
BFF downloads only allowlisted release artifacts, verifies and caches them by
digest, then serves the entrypoint in an opaque-origin sandbox iframe. The
iframe uses a nonce-bound `postMessage` Host API and never receives provider
credentials.

## Static visualization boundary

Each module declares the minimum Dex Go SDK it needs. Dex Server is backward
compatible with every earlier SDK release, so an application may select any
newer stable SDK. CI checks factory execution and visualization with the
latest stable Dex CLI and builds the current source with the latest stable Dex
Go SDK. Dex CLI recognizes the canonical static factory form documented in
runnable examples. Dynamic Step types, branch collections, or targets cannot be
represented reliably by static analysis.

## UI/UX

React primitives, Connector Studio behavior, public catalog format, directory
URLs, search, and connector detail pages are unchanged. Large-directory
virtualization or pagination is a separate performance concern.
