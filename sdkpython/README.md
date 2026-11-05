# Python Dex Connector SDK core

`dex-connectors-sdk` is the Python core for Dex-native provider connectors. It
uses the PEP 420 `dex_connectors` namespace and imports as `dex_connectors.sdk`.
Version `0.7.0` supplies the models, provider operation contracts, credential
helpers, OAuth token exchanges, portable text generation, and single-dispatch
checkpoint values below, plus opt-in provider address policies. Query and
Mutation Step factories use the released Dex SDK's instance-addressed async Steps.
Dex Web FDG 2.0 connector metadata for Python requires upstream R4.

Python 3.11 or newer and `dex-python-sdk>=1.28.0` are required. APIs are
async-first where they perform I/O. Connector Steps run in `AsyncWorker` and
never place credentials in Flow state.

Runnable examples are [`examples/core.py`](examples/core.py),
[`examples/credentials_and_project.py`](examples/credentials_and_project.py),
[`examples/operations_and_oauth.py`](examples/operations_and_oauth.py),
[`examples/single_dispatch.py`](examples/single_dispatch.py), and
[`examples/text_generation.py`](examples/text_generation.py).
[`examples/manifest_refresh.py`](examples/manifest_refresh.py) demonstrates
manifest-declared refresh with an inert provider endpoint.
[`examples/provider_address_policy.py`](examples/provider_address_policy.py)
exercises an address-checked request against a local server.
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

`ManifestCredentialRefreshDriver` reads the generated manifest authorization
declaration and the generated credential field map. It performs refresh-token,
client-credentials, and JWT-bearer grants, retains rotated refresh material,
checks returned scopes, and replaces only mapped credential fields. The caller
supplies an `httpx.AsyncClient` and startup-pinned connection settings; project
credential providers still own admission and persistence. Its constructor takes
`provider_name`, the manifest `spec.auth` object, a mapping from manifest field
names to frozen dataclass attributes, and the caller-owned HTTP client. Optional
`settings` resolves manifest setting sources, and optional `now` supplies a UTC
clock for expiry and JWT claims. A credential dataclass for an authorization
declaration with several `methods` carries `auth_method_id`; an empty value
selects the declaration's `defaultMethod`.

The manifest's `storedMissingExpiry: refresh` declaration refreshes a stored
access token without recorded expiry; `keep` is the default and leaves a
present token in place. The separate `missingExpiry` declaration controls
token responses without `expires_in`.
The driver accepts
`acceptsMissingTokenType: true` in a refresh declaration for a provider that
omits `token_type`. A provider such as Slack that returns `bot` or `user` must
declare grant `acceptedTokenTypes`. Refresh validation uses `requiredScopes`
when declared, and a grant can override `exactScopes` or
`acceptsMissingTokenType`. `acceptedScopeSets` applies to every grant under
`refresh.grants` and excludes `offline_access` from response checks.
`refresh.scopeSeparators` selects `whitespace`
(the default) or `whitespaceAndComma` for both exact and all-scope checks.
A response lacking `expires_in` is retryable by default or with
`missingExpiry: reject`; `keep` returns an expired result, while a positive
declared lifetime supplies its expiry. The default requires `Bearer`.
Only `refresh.allowsPublicClient: true` permits a public-client refresh request
with a blank client secret; other blank secrets require reauthorization before
any provider request.
Missing grant material and declared terminal provider errors raise
`ReauthorizationRequiredError`; transport, invalid response, and undeclared
provider errors preserve the stored credential for retry. A response without
`expires_in` returns an expired result when the manifest declares `keep`; the
current project provider rejects that result without changing stored material.

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
Its optional `address_policy` is `"unrestricted"` by default for compatibility.
`"publicHTTPS"` allows HTTPS only when every resolved address is public;
`"privateNetworkHTTP"` also allows HTTP to private or loopback addresses.
Both restricted policies reject link-local, metadata, multicast, unspecified,
and broadcast addresses. `publicHTTPS` also rejects shared addresses. They
resolve and check every DNS answer, then dial a checked IP while retaining the
URL hostname for Host, TLS
SNI, and certificate verification. DNS resolution and every dial share one
connect timeout. Restricted clients accept only safe `httpx.AsyncClient` options;
they refuse custom transports, applications, mounts, and proxies, including
the legacy `proxies` option. `SSL_CERT_FILE` and `SSL_CERT_DIR` supply trusted
certificates without enabling environment proxies. The restricted client uses
one HTTP/HTTPS connection pool with the standard 100 connection and 20 keepalive
defaults, or caller-supplied `limits`. An `AddressRefusal` contains a safe rule,
never a host or secret. DNS and connect failures before dispatch produce
`ProviderTransportError.provably_not_sent=True`. The caller closes the client.
For example, [the runnable policy example](examples/provider_address_policy.py)
uses `new_provider_http_client(address_policy="privateNetworkHTTP")` for a
loopback HTTP service.
`read_bounded_body` rejects an oversized response. `read_error_tokens` extracts
only safe machine tokens from bounded JSON error bodies, and
`parse_retry_after` accepts delta seconds or HTTP dates capped at one hour.
`send_traced` reports `ProviderTransportError.provably_not_sent` after a traced
DNS, TCP connect, or TLS handshake failure before HTTP request headers are sent.
Failures after header dispatch stay possibly sent, matching the Go connectors'
DNS, connect, TLS, and obtained-connection trace.
`BodyTooLargeError` retains the response status and headers for classification
without putting header values in its message.
The caller closes the streaming response returned by `send_traced`; the bounded
reader closes it on success and failure.
The core example sends through an in-process HTTP transport.

## Version history

- `0.7.0`: Manifest token endpoint templates apply declared
  `templateValueRules` before path escaping. Rules trim Go Unicode whitespace,
  reject names, bound UTF-8 length, accept full-match patterns, and lowercase
  accepted values. Invalid values require reauthorization before a token request.
  `trimClientID` on refresh or client-credentials declarations trims the client
  ID sent to the token endpoint when the Go driver does so.
  Private-network HTTP token endpoints reject fragments and non-printable or
  non-ASCII URL text before transport.
- `0.6.0`: Generated timestamp inputs follow Go `time.Time.UnmarshalJSON`.
  The string mode preserves every supplied fractional digit; the default
  `datetime` mode names a Go-valid value that cannot be represented exactly.
  Manifest grants follow Go scope and credential URL policies for HubSpot,
  Microsoft Graph, Zoho, Salesforce, and monday.com. `responseScopes: ignore`
  skips reported scopes; the default checks them. Absent or null HubSpot scope
  arrays skip the array check; each present element is one complete scope name.
  Null response URLs reuse the validated stored
  origin. Private-network token endpoints reject forbidden addresses before
  connecting. Retryable token failures raise `RetryError`.
- `0.5.0`: Manifest refresh accepts alternative scope sets and validates
  declared delegated JWT subjects using Go `mail.ParseAddress` address equality
  before token requests. `missingExpiry: reject` requires a positive
  `expires_in`; a malformed successful response remains retryable.
- `0.4.0`: Added `storedMissingExpiry` for refresh-token, client-credentials,
  and JWT-bearer grants. A stored token without expiry can now trigger refresh
  even when a replacement response must include `expires_in`. `missingExpiry`
  no longer implicitly triggers refresh of a stored token without expiry.
- `0.3.0`: Added opt-in provider address policies and text generation policy
  forwarding. The unrestricted default keeps the `0.2.0` network behavior.
- `0.2.0`: Added portable text generation and single-dispatch checkpoints.

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

## Portable text generation

`dex_connectors.sdk.textgen` provides the provider-neutral `generateText`
request, response, six branch definitions, schema validation, model ID rules,
error rules, and async `TextGenerationQuery`. Its `openaichat` adapter accepts a
declarative `Profile` for Chat Completions providers. The Query validates
locally before resolving a credential, sends it only in the configured header,
heartbeats every five seconds during an attempt, bounds bodies and stream
events, and classifies transport failures as retries. Only `generated` is a
required branch. `truncated`, `blocked`, `providerRejected`, `invalidResponse`,
and `defect` are optional. The generated text Stream can contain provisional
deltas from an attempt that later retries; the Result text is authoritative.

The [runnable example](examples/text_generation.py) uses a local HTTP mock to
exercise the complete Query without provider credentials. A connector supplies
its generated `QueryDefinition`, model and base URL, credential resolver, and
provider `Profile`. Its Step defaults must select sync Execute durability, a
heartbeat timeout of zero or at least ten seconds, and an Execute timeout
longer than the request timeout. Run the example with
`uv run python examples/text_generation.py` from `sdkpython/`.

The example exercises the SDK and mock HTTP exchange. A connector port still
needs its own runnable Dex Flow and provider verification.

### Request and result contract

`TextGenerationRequest.messages` is an ordered, nonempty tuple of `Message` values.
Each `Message.role` is `user` or `assistant` (`MessageRole.USER` and
`MessageRole.ASSISTANT`), and `Message.text` must be nonempty. The optional
`model` overrides the connection model after trimming. `instructions` precedes
the conversation when supported. `max_output_tokens` is a nonnegative count;
zero leaves the provider limit unset. `temperature` is a finite, nonnegative
sampling value or `None` to omit it. `reasoning_effort` may be empty to omit it,
or one of `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max` when the
model's rules support it. These are the values of `ReasoningEffort`.

`StructuredOutput.name` is an ASCII name of 1–64 letters, digits, `_`, or `-`;
`schema` is the JSON object checked against the portable subset below, and
`description` is optional provider-facing guidance. `TextGenerationResponse`
holds `text`, `requested_model`, `served_model`, `response_id`,
`finish_reason`, `provider_finish_reason`, and `usage`. Text is populated only
on `generated` and `truncated`. The portable `FinishReason` values are `stop`,
`length`, `contentPolicy`, and `refusal`. `Usage` contains nonnegative
`input_tokens`, `cached_input_tokens`, `output_tokens`, `reasoning_tokens`, and
`total_tokens`; absent counts are zero and absent total is computed from input
plus output. `TextGenerationResult` aliases `QueryResult[TextGenerationResponse]`.

The stable operation ID is `generateText`. Branch IDs are `generated`,
`truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect`.
`text_generation_branch_definitions()` returns their definitions in manifest
order; only `generated` is required. `ModelIDRule.BODY` accepts 1–256 printable
ASCII characters without spaces; `PATH_SEGMENT` accepts up to 128 letters,
digits, `.`, `_`, or `-` after an optional `models/` prefix. `resolve_model`
uses the request model before the connection default.

The portable schema accepts only `type`, `properties`, `required`,
`additionalProperties`, `items`, `enum`, `const`, `description`, `title`,
`minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, `maxItems`, and
`format`. The root is an object with every property required and
`additionalProperties: false`. Nested nodes may use one JSON type or a pair
of a type and `null`. Depth is at most 10; count bounds are nonnegative
integers below 2⁶³; numbers are finite and limited to 256 literal characters
and exponent magnitude 400. String formats are RFC 3339 `date-time`, `date`,
and `time`, or a hyphenated `uuid`. Fractional seconds may use `.` or `,` as
the separator. `validate_portable_request` checks locally;
`extract_structured_answer` removes an optional JSON fence and checks the
original schema, raising `ValueError` with a JSON pointer and no answer text.

### Query pipeline and provider adapters

Every `TextGenerationQuery.invoke` performs these steps in order, and reports
the first local error in this order: (1) resolve and validate the model;
(2) validate messages, output count, schema, declared features, temperature,
and reasoning effort; (3) apply the model's schema mode and transformations;
(4) resolve and validate the credential; (5) encode and validate the provider
request; (6) dispatch with five-second heartbeats and byte limits; (7)
classify a non-2xx response; (8) decode a 2xx response, map its finish reason,
join non-reasoning text, and validate structured output; (9) return the branch
and safe receipt. Steps 1–5 select `defect` without a provider request. A
transport failure, stall, interrupted stream, or retryable status returns
Retry. Each invocation owns its mutable state. A supplied HTTP client remains
the caller's property and must stay open; otherwise a client is opened and
closed per invocation, so connections are not pooled across calls.

`TextGenerationQueryConfig` supplies `definition` (the six branches and sync
Execute defaults), `wire_format`, HTTPS `base_url`, `connection_model`,
`request_timeout_seconds` (a whole-exchange deadline including body reads),
`resolve_credential`, `max_response_bytes`, `max_stream_event_bytes` (required
for streaming), optional caller-owned `http_client`, and `address_policy`.
The policy defaults to unrestricted. A restricted policy requires the Query
to own its HTTP client; a supplied client cannot bypass the check. The Execute
timeout must exceed the request timeout; a nonzero heartbeat timeout is at least ten
seconds. The Query's `definition`, `request_features`, and `model_id_rule`
methods expose those declarations without making a provider request.

`WireFormat` declares `provider_name` for failures and receipts,
`model_id_rule`, `features`, `credential_header`, `rules_for_model`,
`encode_request`, `decode_response`, optional `decode_stream`,
`finish_reasons`, ordered `error_rules`, `error_token_pointers`, optional
`read_error_retry_delay`, `request_id_headers`, `rate_limit_headers`, and
`stall_timeout_seconds`. Its callbacks must be safe for concurrent calls.
`RequestFeatures` has five false-by-default predicates named
`supports_instructions`, `supports_structured_output`,
`supports_max_output_tokens`, `supports_temperature`, and
`supports_reasoning_effort`; an unsupported nonempty request field selects
`defect`. `ModelRequestRules` supplies `temperature`, `reasoning_efforts`, and
`structured_output`. `TemperaturePolicy.accepted` defaults true;
`minimum` and `maximum` are optional inclusive bounds. The helper functions
`temperature_accepted`, `temperature_range`, and `temperature_not_accepted`
make those choices. `StructuredOutputRules.mode` defaults to `NONE`, which
rejects a structured request; `JSON_SCHEMA` sends a schema, and
`JSON_OBJECT_WITH_INSTRUCTION` sends a JSON-object mode with a schema
instruction. Its `keywords_moved_to_description` and `keywords_removed`
change only the provider-facing copy; local validation uses the original.

`CredentialHeader.name` and `prefix` form the only credential-bearing header.
`EncodeRequestInput.request` and `.rules` carry validated values to the
encoder. `EncodedRequest.path`, `.body`, `.headers`, and `.is_streaming`
describe one credential-free request. `DecodedResponse.parts` is an ordered
tuple of `ResponsePart.text` and `.is_reasoning`; its `served_model`,
`response_id`, `provider_finish_reason`, `is_refusal`, and `usage` are checked
before branch selection. `ProviderReportedError.error_tokens` holds bounded
machine tokens from an error in a 2xx response. `ErrorOutcome.disposition`
and `.kind` select Retry or a terminal branch. `ErrorRule.status_code` is a
specific non-2xx status, or zero for any status and reported 2xx errors;
`error_token` matches one extracted token exactly, or is empty for any token;
`outcome` is the selected classification. The first matching rule wins.
Without a rule, 408, 429, and 5xx except 501 retry; 401, 403, 404, 409,
and 402 select provider rejection with their respective failure kinds; 3xx
selects `invalidResponse`; other statuses select provider rejection. An
unmatched 2xx reported error selects `invalidResponse`.

`decode_response` receives a complete bounded body. `decode_stream` receives
an async iterator of raw response bytes and a synchronous text-delta writer;
an adapter must parse SSE itself when needed. It should raise
`ProviderReportedError` for a provider error object, `EOFError` for a stream
interrupted before its terminal event, `httpx.HTTPError` for a response read
failure, or `ValueError` for malformed provider data. The Query maps the first
two transport cases to Retry, malformed data to `invalidResponse`, and a text
Stream write failure to Retry with availability. Decoder error strings are not
copied into failures. `stall_timeout_seconds` is an optional interval between
received bytes; zero disables it, leaving the whole-request timeout to bound
the attempt. Keep-alives count as received bytes.

### Chat Completions profile

`openaichat.Profile.provider_name` is required. `chat_completions_path`
defaults to `/chat/completions`; `credential_header` defaults to Authorization
with Bearer prefix; `fixed_headers` contains safe nonsecret constants.
`instructions_role` defaults to `SYSTEM` (`system`) and may be `DEVELOPER`
(`developer`). `max_tokens_field` defaults to `MAX_TOKENS` (`max_tokens`) and
may be `MAX_COMPLETION_TOKENS` (`max_completion_tokens`). `reasoning_efforts`
maps supported portable efforts to wire values; an empty map rejects effort.
`temperature` accepts finite nonnegative values by default;
`structured_output` rejects schema requests by default.
`should_send_strict_json_schema` adds `strict: true`. `streaming` defaults to
`NEVER`; `ALWAYS` requests SSE, and `should_request_stream_usage` adds usage
stream options. `allowed_request_fields`, when nonempty, lists all accepted
top-level body fields and must include `model`, `messages`, and required stream
fields. Unknown request fields select `defect`. `finish_reasons` extends the
default `stop`, `length`, and `content_filter` mapping. `error_rules`,
`error_token_pointers`, `request_id_headers`, and `rate_limit_headers` control
classification and safe receipt metadata. `stall_timeout_seconds` sets the
between-byte stall interval. `model_rules` applies the first matching
`ModelRule`: exactly one `model_id_prefix` or anchored `model_id_pattern`
selects the model; optional `temperature`, `reasoning_efforts`,
`structured_output`, `max_tokens_field`, and `streaming` override the profile.
`new_wire_format` validates the profile before constructing the adapter.

The adapter accepts missing or null Chat messages and stream deltas as empty
objects, then applies the finish reason. Usage counts accept integral JSON
numbers and quoted numeric strings. Malformed choices invalidate the whole
response, including choices after the first.

The Python adapter currently parses SSE inside `decode_stream`; there is no
public shared SSE reader in this release.

`dex_connectors.sdk.textgen.textgentest` provides `FakeProvider` for
credential-safe HTTP exchange tests. `FakeReply` configures status, headers,
body, an initial delay in seconds, delayed `FakeStreamChunk` values, or a
dropped connection. `RecordedRequest` exposes method, path, redacted headers
and body, and predicates for credential placement. `client()` returns a
caller-owned `httpx.AsyncClient`; `enqueue_replies()` scripts ordered replies;
`requests()` returns sanitized records. `run_text_generation_exchange_suite()`
checks a normal finish, malformed response, retry, and credential placement
against a connector's Query factory. A consuming connector supplies additional
branch cases and a real Dex Flow test for Worker, Client, retry, and Stream
behavior.

## 0.1.0 release notes

First Python release of the provider-neutral Connector SDK: Go-compatible
identities, failures, results, retry defaults, project configuration,
credential resolution, provider HTTP, connector mocks, async operation
contracts, OAuth token exchange, Query and Mutation Step registration,
result Attribute and progress Streams, and the full single-dispatch fence.
Real `dexcli dev` Flows verify the async Step paths. Python FDG metadata
awaits the upstream R4 analyzer; it is not generated by this release.
