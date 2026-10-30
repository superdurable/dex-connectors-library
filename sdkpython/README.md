# Python Dex Connector SDK core

`dex-connectors-sdk` is the Python core for Dex-native provider connectors. It
uses the PEP 420 `dex_connectors` namespace and imports as `dex_connectors.sdk`.
Version `0.1.0` is unreleased. The package supplies the models, provider
operation contracts, credential helpers, OAuth token exchanges, and
single-dispatch checkpoint values below. Dex Step factories and the
single-dispatch heartbeat fence require the upstream R1–R3 Dex SDK release.
Dex Web FDG 2.0 connector metadata for Python requires upstream R4.

Python 3.11 or newer and `dex-python-sdk>=1.26.0` are required. APIs are
async-first where they perform I/O. The package has no Dex Worker dependency in
its current execution path and never places credentials in Flow state.

Runnable examples are [`examples/core.py`](examples/core.py),
[`examples/credentials_and_project.py`](examples/credentials_and_project.py),
and [`examples/operations_and_oauth.py`](examples/operations_and_oauth.py).
Run each with `uv run python examples/<name>.py`.

## Core values, failures, and retry policy

`CallID` is a UUID string stable across retries. `Call` binds that identity to
the logical `ConnectionRef` and `OperationRef`. Its `AsyncContext` field is live
only during a Step handler and never enters JSON or Flow state. `QueryResult`,
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
authorization is required. `StaticCredentialProvider` resolves an in-memory
connection map for examples and tests. `resolve_credential` delegates to
coordinated refresh when a provider supports it;
`resolve_credential_after_rejection` requires that support and should be called
at most once after a provider authentication rejection. Refreshing providers
serialize per connection, reload after taking that lock, and persist complete
replacement credentials atomically.

From the runnable [operation example](examples/operations_and_oauth.py):

```python
credentials = StaticCredentialProvider({connection: SecretString("INERT_SECRET")})
credential = await resolve_credential(credentials, call, ExampleRefreshDriver())
assert credential.reveal() == "INERT_SECRET"
```

## Provider operations

`Query` and `Mutation` are async provider contracts independent of Dex.
Each has `definition()` and `invoke(call, input)`; a Mutation also derives a
stable `idempotency_key(call_id, input)`. `run_query` and `run_mutation` validate
the attempt against declared branches, complete the safe receipt, and raise
`RetryError` for a safe-to-repeat failure. A returned unknown branch or foreign
receipt selects `defect`. An `uncertain` Mutation requires a declared route and
the narrow unconfirmable-write conditions in the repository rules. Retry
exhaustion is handled by Dex and fails the Flow.

From the same runnable example:

```python
result = await run_mutation(CreateExampleItem(), call, "item-1")
assert result.branch == "created" and result.receipt.idempotency_key == str(call_id)
```

## OAuth token refresh

`oauthtoken.TokenEndpoint` performs bounded, redirect-disabled refresh-token,
client-credentials, and JWT-bearer exchanges. The connector owns its endpoint,
terminal error codes, scopes, and credential type. A declared terminal grant
error below HTTP 500 raises `ReauthorizationRequiredError`; server outages and
other failures stay retryable. `TokenResponse.next_refresh_token` retains the
prior refresh token when a provider does not rotate it. The package also has
RS256 JWT assertion signing, scope checks, and proportional expiry skew.

From the runnable example:

```python
endpoint = TokenEndpoint("Example", "https://oauth.example/token", http_client)
oauth_client = ClientCredentials(
    "example-client", SecretString("INERT_CLIENT_SECRET"),
    ClientAuthenticationMethod.CLIENT_SECRET_POST,
)
token = await endpoint.exchange_refresh_token(oauth_client, SecretString("INERT_REFRESH"))
assert token.access_token.reveal() == "INERT_ACCESS"
```

## Single dispatch checkpoints

`singledispatch.Marker` uses the Go heartbeat JSON shape. `decide_dispatch`
allows an initial send or a resend only after the immediately prior attempt
recorded `notSent` or `canResend` without a provider ID. A claimed, missing,
stale, or accepted checkpoint requires read-back; malformed or foreign data
fails closed. The Dex heartbeat fence will use these values after R1 lands.

```python
assert decide_dispatch(call_id, 1, None).kind == DecisionKind.SEND_ALLOWED
```

## Provider HTTP

`new_provider_http_client` returns an `httpx.AsyncClient` with redirects
disabled so authorization headers are never replayed to another host.
`read_bounded_body` rejects an oversized response. `read_error_tokens` extracts
only safe machine tokens from bounded JSON error bodies, and
`parse_retry_after` accepts delta seconds or HTTP dates capped at one hour.
`send_traced` reports `ProviderTransportError.provably_not_sent` only after a
traced pre-send connection failure; all other transport failures fail closed.
`BodyTooLargeError` retains the response status and headers for classification
without putting header values in its message.
The caller closes the streaming response returned by `send_traced`; the bounded
reader closes it on success and failure.
The core example sends through an in-process HTTP transport.

## Project configuration

`load_from_environment()` reads `DEX_CONNECTOR_CONFIG_FILE` for Dex Web local
development or the trusted `DEX_PROJECT_*` hosted S3 contract. Local
`connections.json` and `use-configurations.json` must be regular owner-only
`0600` files. Hosted reads pin an exact version and SHA-256 digest, and
`ConfigurationStore` uses revision CAS. `S3ObjectStore.create` checks bucket
versioning and its KMS boundary. Versioned documents use Go-compatible compact
JSON bytes so Unicode values produce the same snapshot digests.
`RFC3339NanoTimestamp` preserves provider offsets and nanoseconds when a
credential or receipt is decoded. `LoadedProject.resolve_credential` reads
credentials at call time; shared connections use the credential broker
protocol, with a forced revision read after provider rejection. Never log or
persist the returned credential object. The examples freeze an in-memory
snapshot, resolve a private application secret and broker credential, load an
owner-only Dex Web file, and exercise S3's conditional-write adapter.

`projectconfig.provider.CredentialProvider` binds a non-refreshing connector
credential decoder to a stored connection. `RefreshingCredentialProvider` also
takes an encoder for complete replacement material. It conditionally admits one
refresh request per credential generation across replicas, stores an immutable
result, then publishes it through a revision CAS. Concurrent callers recover
the result without another provider request. A retryable refresh failure
restores the prior credential; a terminal grant failure fences it for
reauthorization. A generic provider 401 can force refresh only after the
authoritative stored expiry has elapsed. Under `dexcli dev`, the same provider
uses process-local connection heads and writes refreshed material back to the
owner-only `connections.json` file only while it still holds the prior
credential. A newer Dex Web authorization wins the race.

From [the runnable project example](examples/credentials_and_project.py):

```python
refreshing = RefreshingCredentialProvider(
    project_connections, key, lambda value: value,
    lambda value: value,
)
refreshed = await resolve_credential(refreshing, call, ExampleProjectRefreshDriver())
assert refreshed["access"] == "INERT_NEW_ACCESS"
assert (await project_connections.read_connection(key)).status == "READY"
```

## Connector mocks

`ScriptedOperation` holds a provider Query or Mutation script without Dex Step
registration. `respond`, `when`, `default`, `strict`, and `for_flow` model the Go
mock's precedence. Keyed terminal Mutation outcomes replay; retries consume
the next case. `load_fixture` strictly reads JSON fixture objects and rejects
duplicate fields. `assert_no_failures()` fails a test after an unscripted call
or invalid page; `DEX_CONNECTOR_MOCK_REPORT_DIR` enables safe call rows in
`connector-calls.json`. In pytest, pass `report_failure=pytest.fail` to
`ScriptedOperation` so a missing script fails immediately. The core example
answers a Query with a declared branch.
`connectormock.Query` and `connectormock.Mutation` wrap that script as real
async operation implementations using the connector's actual definition.
Their `invoke` requires the live `Call.context` so Flow-scoped scripts use
Dex's Flow ID. Step factory integration tests will exercise them after R1–R3.

## Verification

Run `uv sync`, `uv run pytest`, `uv run mypy --strict src tests examples`, and
`uv run ruff check src tests examples`. Real Dex Worker integration for Step
factories and the heartbeat fence waits for the upstream R1–R3 release.

The provider address-policy client and SSE reader are outside this release.

## 0.1.0 release notes

First Python release of the provider-neutral Connector SDK: Go-compatible
identities, failures, results, retry defaults, project configuration,
credential resolution, provider HTTP, connector mocks, async operation
contracts, OAuth token exchange, and single-dispatch checkpoint values.
Dex Step registration, live Flow verification, and Python FDG metadata remain
pending the upstream Dex SDK and CLI releases described above.
