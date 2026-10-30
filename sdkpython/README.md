# Python Dex Connector SDK core

`dex-connectors-sdk` is the Python core for Dex-native provider connectors. It
uses the PEP 420 `dex_connectors` namespace and imports as `dex_connectors.sdk`.
Version `0.1.0` is unreleased. Phase 1a supplies the models and protocols below;
Step factories, Triggers, singledispatch, text generation, and OAuth refresh
drivers wait for the Dex SDK R1–R4 gaps described in the repository's design.

Python 3.11 or newer and `dex-python-sdk>=1.26.0` are required. APIs are
async-first where they perform I/O. The package has no Dex Worker dependency in
its current execution path and never places credentials in Flow state.

The runnable examples are [`examples/core.py`](examples/core.py) and
[`examples/credentials_and_project.py`](examples/credentials_and_project.py).
Run them with `uv run python examples/core.py` and
`uv run python examples/credentials_and_project.py`.

## Core values, failures, and retry policy

`CallID` is a UUID string stable across retries. `Call` binds that identity to
the logical `ConnectionRef` and `OperationRef` without a Dex context in JSON. `QueryResult`,
`MutationResult`, `QueryDefinition`, and `MutationDefinition` encode the same
camel-case JSON as `sdkgo`, including optional
failure omission and RFC 3339 receipt timestamps. `FailureKind` describes a
fact; an operation decides whether to choose a branch or retry. The default
Execute retry policy starts at one second, doubles to five minutes, has no
attempt count limit, and ends after one hour. Exhaustion fails the Flow.

The core example constructs and round-trips a `QueryResult`, models a retry
attempt, and reads the default policy. `MutationAttempt.uncertain` is reserved
for a possibly accepted write that cannot be repeated or confirmed by read-back.

## Configuration UI

`ConnectorConfigurationUI` contains ordered `ConnectorUIUnit` records. Each
unit requires an instructional description and one or more
`ConnectorUIBinding` JSON Pointers. The core example constructs and validates a
complete unit. It carries presentation metadata only; configuration values
are loaded by `projectconfig` at application startup.

## Credentials

`CredentialProvider` is an async Protocol that resolves one `Call` at provider
call time.
`CredentialRefreshDriver` declares a deterministic refresh decision and async
exchange but does not store results. `SecretString` redacts string and repr
rendering and rejects serialization; call `reveal()` only when building a
provider request. The examples implement both credential Protocols and show
safe rendering. A connector
should return `ReauthorizationRequiredError` only when interactive provider
authorization is required.

## Provider HTTP

`new_provider_http_client` returns an `httpx.AsyncClient` with redirects
disabled so authorization headers are never replayed to another host.
`read_bounded_body` rejects an oversized response. `read_error_tokens` extracts
only safe machine tokens from bounded JSON error bodies, and
`parse_retry_after` accepts delta seconds or HTTP dates capped at one hour.
`send_traced` reports `ProviderTransportError.provably_not_sent` only after a
traced pre-send connection failure; all other transport failures fail closed.
The core example sends through an in-process HTTP transport.

## Project configuration

`load_from_environment()` reads `DEX_CONNECTOR_CONFIG_FILE` for Dex Web local
development or the trusted `DEX_PROJECT_*` hosted S3 contract. Local
`connections.json` and `use-configurations.json` must be regular owner-only
`0600` files. Hosted reads pin an exact version and SHA-256 digest, and
`ConfigurationStore` uses revision CAS. `S3ObjectStore.create` checks bucket
versioning and its KMS boundary. Versioned documents use Go-compatible compact
JSON bytes so Unicode values produce the same snapshot digests. `LoadedProject.resolve_credential` reads
credentials at call time; shared connections use the credential broker
protocol, with a forced revision read after provider rejection. Never log or
persist the returned credential object. The examples freeze an in-memory
snapshot, resolve a private application secret and broker credential, load an
owner-only Dex Web file, and exercise S3's conditional-write adapter.

## Connector mocks

`ScriptedOperation` holds a provider Query or Mutation script without Dex Step
registration. `respond`, `when`, `default`, `strict`, and `for_flow` model the Go
mock's precedence. Keyed terminal Mutation outcomes replay; retries consume
the next case. `load_fixture` strictly reads JSON fixture objects and rejects
duplicate fields. The core example answers a Query with a declared branch.

## Verification

Run `uv sync --extra dev`, `uv run pytest`, `uv run mypy --strict src tests examples`, and
`uv run ruff check src tests examples`. Real Dex Worker integration belongs to
Phase 1b after the upstream SDK gaps are addressed and runs in CI.
