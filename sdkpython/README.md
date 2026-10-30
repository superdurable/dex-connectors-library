# Python Dex Connector SDK core

`dex-connectors-sdk` is the Python core for Dex-native provider connectors. It
uses the PEP 420 `dex_connectors` namespace and imports as `dex_connectors.sdk`.
Version `0.1.0` supplies the models, provider
operation contracts, credential helpers, OAuth token exchanges, and
single-dispatch checkpoint values below. Query and Mutation Step factories
use the released Dex SDK's instance-addressed async Steps.
Dex Web FDG 2.0 connector metadata for Python requires upstream R4.

Python 3.11 or newer and `dex-python-sdk>=1.28.0` are required. APIs are
async-first where they perform I/O. Connector Steps run in `AsyncWorker` and
never place credentials in Flow state.

Runnable examples are [`examples/core.py`](examples/core.py),
[`examples/credentials_and_project.py`](examples/credentials_and_project.py),
[`examples/operations_and_oauth.py`](examples/operations_and_oauth.py),
and [`examples/single_dispatch.py`](examples/single_dispatch.py).
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
`RetryError` for a safe-to-repeat failure. A returned unknown branch, foreign
receipt, or invalid attempt selects `defect`. An `uncertain` Mutation requires a declared route and
the narrow unconfirmable-write conditions in the repository rules. Retry
exhaustion is handled by Dex and fails the Flow.
`RetryError.retry_after_whole_seconds` truncates positive fractional delays to
the whole seconds accepted by Dex; zero means no explicit retry hint.

From the same runnable example:

```python
result = await run_mutation(CreateExampleItem(), call, "item-1")
assert result.branch == "created" and result.receipt.idempotency_key == str(call_id)
```

## Connector Steps

`QueryStep` and `MutationStep` are async Dex `Step` instances configured with
their corresponding `QueryStepConfig` or `MutationStepConfig`. The instance's
stable `step_type` and concrete `step_input_type` let Dex register multiple
instances of one generic class. A required branch needs a target; an unwired
optional branch fails the Flow with a bounded, secret-safe reason. `StepOptions`
overrides may change Execute settings but cannot add WaitFor behavior. The
factory maps a connector `RetryError` to Dex retry, truncating RetryAfter to
whole seconds; retry exhaustion remains a failed Flow. A configured result
Attribute is written before routing and belongs in the Flow persistence schema.
`ProgressUpdate` Stream messages include call ID, Dex attempt, and monotonic
connector sequence. `Call.report_progress(Progress(...))` emits them when a
progress Stream is configured; `Call.write_text` writes ordered buffered text
when a text Stream is configured. These outputs are best effort and belong in
the consuming Flow persistence schema. The default text flush interval is one
second with a 16 KiB buffer; Step configs can override both settings.

From [the runnable operation example](examples/operations_and_oauth.py):

```python
created = CreatedItem()
result_attribute = dex.Attribute("createdItem", MutationResult[str])
progress_stream = dex.Stream("createProgress", ProgressUpdate, 64 * 1024)
text_stream = dex.Stream("createText", str, 64 * 1024)
create = MutationStep(MutationStepConfig(
    "CreateItem", str, StepAnnotations("sample", "Sample", "Create one sample item."),
    CreateExampleItem(), connection, lambda value: value,
    {"created": created},
    result_attribute=result_attribute,
    progress_stream=progress_stream, text_stream=text_stream,
))
dex.Registry([
    CreateItemFlow(create, created, result_attribute, progress_stream, text_stream)
], allow_async_handlers=True)
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
fails closed. `singledispatch.Fence` reads `Call.context.deadline` and the
previous Dex heartbeat before credential lookup. `dispatch(send)` checkpoints
`claimed` before one provider send and repeats that claim every five seconds.
The callback receives the write deadline and must avoid transport replay.
Call `record_not_sent` before retrying a credential or pre-send failure;
`record_can_resend` requires positive proof that the provider did not process
the request. `record_provider_id` preserves an accepted resource identity for
read-back. Use sync Execute durability because local async activities ignore
heartbeats.

From [the runnable single-dispatch example](examples/single_dispatch.py):

```python
fence = Fence(call, timedelta(seconds=10))
assert (await fence.decide()).kind == DecisionKind.SEND_ALLOWED

async def send(write_deadline: datetime) -> str:
    """Use the deadline when constructing a real non-replaying provider request."""
    assert write_deadline > datetime.now(UTC)
    return "INERT_PROVIDER_OBJECT"

provider_id = await fence.dispatch(send)
await fence.record_provider_id(provider_id)
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
Providers built with `project.connections` route a `sharedConnectionId` to
the credential broker. They never read a stored connection head or call the
refresh driver for that entry. Pass `decode_shared` when the broker omits
host-kept renewal secrets required by the ordinary decoder. Broker
`REAUTHORIZATION_REQUIRED` and `CONNECTION_NOT_SELECTED` propagate to callers.

From the same runnable example:

```python
shared_provider = ProjectCredentialProvider(
    project.connections,
    ConnectionKey("sample", "main"),
    lambda value: {"apiKey": str(value["apiKey"]), "privateKey": str(value["privateKey"])},
    decode_shared=lambda value: {"apiKey": str(value["apiKey"])},
)
assert (await shared_provider.resolve(call))["apiKey"] == "REDACTED_SHARED_KEY"
```

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
Dex's Flow ID. Real Dex Flow tests exercise keyed terminal replay after a
lost mock response.

## Verification

Run `uv sync`, `uv run pytest`, `uv run mypy --strict src tests examples
integration`, and `uv run ruff check src tests examples integration`. For real
Dex Flow tests, install the latest stable CLI with
`python3 ../script/dex_compatibility.py install-dexcli --output ../.bin/dexcli`,
then from `sdkpython/` run `mkdir -p .dexcli-data/logs` and start
`../.bin/dexcli dev -dex-port 19881 -web-port 19882 -open=false
-zd-data-dir .dexcli-data/zd -server-log-folder .dexcli-data/logs`. Run
`make test-sdkpython-integration` from the repository root. CI runs this suite
for Python SDK changes against the Dex CLI selected once for the workflow run.
The integration suite checks
branch routing, retry and exhaustion, uncertainty, the lost-response fence,
safe resend from a persisted checkpoint, heartbeat cadence, mock replay,
result Attribute, and progress/text Streams.

The provider address-policy client and SSE reader are outside this release.

## 0.1.0 release notes

First Python release of the provider-neutral Connector SDK: Go-compatible
identities, failures, results, retry defaults, project configuration,
credential resolution, provider HTTP, connector mocks, async operation
contracts, OAuth token exchange, Query and Mutation Step registration,
result Attribute and progress Streams, and the full single-dispatch fence.
Real `dexcli dev` Flows verify the async Step paths. Python FDG metadata
awaits the upstream R4 analyzer; it is not generated by this release.
