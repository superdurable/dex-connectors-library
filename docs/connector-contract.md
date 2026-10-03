# Dex-native Connector contract v1alpha1

The Go types in `sdkgo` are the executable form of this contract. Connector
modules declare the minimum Dex Go SDK, `github.com/superdurable/dex/sdk-go`,
that they need. Dex Server is backward compatible with every earlier SDK
release, so an application may select any newer stable SDK, and CI checks the
contract with the latest stable Dex CLI and Dex Go SDK. The contract does not
change Dex Server or its database. See
[Dex versions](versioning-and-releases.md#dex-versions).

## Operation and Step factory boundary

A Connector exposes typed `Query[IN, OUT]` and `Mutation[IN, OUT]`
operations. The normal application API is an operation-specific, execute-only
Dex Step factory such as:

```go
openai.NewCreateResponseStep(openai.CreateResponseStepConfig[Input]{
    StepType:  "GenerateSummary",
    Connection: openAIConnection,
    MapToOperationInput: mapToOperationInput,
    Completed: sdkgo.GoTo(CompletedStep{}),
})
```

Generated configs embed the canonical SDK Query or Mutation factory marker.
Each declared operation branch becomes one named, strongly typed config field.
`sdkgo.GoTo` binds its target input type at compile time; the generated
constructor converts it to the operation's internal branch target. The generic
`MustNewQueryStep` and `MustNewMutationStep` APIs remain available only for
custom or dynamic operations.

Each connector exposes its own non-serializable `Connection` type. It binds a
configured client to a validated logical `ConnectionRef`, so an HTTP, OpenAI,
Gmail, or Sheets connection cannot be passed to another connector factory.
The same typed connection can be reused by operations from that connector.

The factory owns one provider invocation and then either one `dex.GoTo` or a
Flow failure. The application supplies a stable Step type, annotations,
registration-time typed Connection, pure `MapToOperationInput`, and one typed
target for every required branch. Optional branches may be left empty.
A target receives only the current Connector Result:

```go
type ListThreadMessagesResult = sdkgo.QueryResult[ListThreadMessagesOutput]
```

Connector Steps do not forward their input. Applications persist durable
business context in domain Attributes or pass an explicit domain value through
an application Step before invoking the next Connector Step.

`GoTo` and `GoToBranch` enforce the target input type at Go compile time. `StepRef[T]`
provides a lightweight reference to another factory by stable Step type; the
real target must be registered and its registered options remain authoritative.
A StepRef fails if it is ever executed as a handler.

`RunQuery` and `RunMutation` remain the lower-level escape hatch for a complex
business Step. They preserve the same identity, attempt, retry, credential,
and Stream contracts. Provider calls occur only in `Execute`, never in
`WaitFor` or RPC.

## Trigger source boundary

Connector Triggers are long-running ingress adapters, not provider calls made
by a Flow. A generated Trigger factory binds a typed provider `TriggerSource`
to an application-owned `TriggerTarget`. The manifest does not classify an
event as a Flow or RPC Trigger. An application may connect the same event type
to a Flow-start target, an RPC target, or another target. Provider code does
not know the application Flow type, RPC method, or business Flow ID scheme.

Every event contains a provider-stable event ID and occurrence time. Sources
must document their acknowledgement, retry, and crash-recovery guarantees.
Before acknowledging a matched provider event, a source calls
`PrepareTriggerDelivery`. Generated local factories use this boundary to fsync
the event to a binding-specific inbox and replay it after restart.
Sources hand each acknowledged event to `sdkgo.DeliverTrigger`, or document an
equivalent policy: nil and `UndeliverableTriggerError` consume the event, and
every other error is retried with backoff. A source must not stop delivering
later events because one event is undeliverable, and it replays pending events
before it delivers new ones. When one runner feeds both a Flow start and that
Flow's RPCs, it delivers every event that can start a Flow before a later event
that targets that Flow. For example, it can deliver events in the order the
provider sends them, or deliver every root before any reply found in the same
poll. Sources document this ordering guarantee alongside acknowledgement,
retry, and crash recovery.
Sources log through `log/slog` with the SDK's message and attribute
conventions (see `sdkgo/README.md`). A provider event the source ignores is
logged at DEBUG with a `reason`, a failed attempt or connection at WARN with
its `attempt` count and next `delay`, and never a payload, message text, or
credential. Retries and reconnects back off, so an outage does not log once a
second, and a healthy idle connection or a routine reconnect that the provider
requests logs no WARN. A source that
consumes an undeliverable event itself logs that skip at WARN. A source that
retries on its own schedule, such as a poller, runs each attempt through
`sdkgo.TriggerAttempt`, so it does not report an event that an inbox or runner
skipped as delivered after a retry. It keeps retrying a failed event until the
event is consumed, even after the provider stops listing it.
Applications supply the Flow ID resolver and start-input mapper, and the SDK
derives the Flow-start request ID from the provider event ID. RPC targets use
the same direct bound Flow method for registration and target invocation. The
application owns RPC options, durable state, locking, and event deduplication.
RPC names are code identities, not binding configuration.

Trigger binding configuration is separate from connection configuration and
credentials. Its identity is connector ID, connection name, trigger name, and
binding name. One OAuth connection may therefore serve several independently
configured Flow triggers.

One Step execution invokes one Connector operation. A second call in the same
execution reuses the same Call ID and is prohibited. Use another Step
execution for a second provider call or polling iteration.

## Branch-only Result and strict Attempt

An operation declares stable branches in its manifest and generated
definition. Every operation declares the standard `defect` branch. A Mutation
declares the standard `uncertain` branch only when dispatch can produce an
outcome that is unsafe to retry.

```go
type QueryResult[T any] struct {
    Branch  BranchID
    Value   T
    Receipt Receipt
    Failure *Failure
}
```

There is no public fixed Outcome enum. Provider-specific branches may express
`found`, `notFound`, `completed`, `rejected`, or other durable Process
vocabulary. A required branch must have exactly one GoTo target. A missing,
duplicate, or unknown required target rejects factory construction. An optional
branch may omit its target. Selecting that branch writes the Result Attribute
when one is configured, then fails the Flow. It does not enter Execute retry.

Operation implementations cannot return an unclassified Go error. They return:

- `NewQueryBranch` or `NewQueryRetry`;
- `NewMutationBranch`, `NewMutationUncertain`, or `NewMutationRetry`.

Only Retry becomes a non-nil Go error and enters the Dex Execute retry policy.
A positive provider delay becomes `dex.RetryAfter`; zero delay uses the Step
policy. A branch or uncertainty returns `error == nil`. A required branch is
routed by the Process. An unwired optional branch fails the Flow.

Mutation uncertainty is not an ordinary caller-selected branch. After a
request dispatch, a lost connection, truncated response, ambiguous server
error, or missing terminal event uses `NewMutationUncertain`; the SDK selects
the standard `uncertain` branch. A Mutation that cannot produce uncertainty
omits that branch and target. Invalid Attempts and local SDK defects fail closed
to the standard `defect` branch.

`FailureKind` records a safe fact, not retry policy. Authentication,
authorization, not-found, rate-limit, transport, protocol, provider rejection,
response limit, validation, availability, and local defect may accompany a
branch. The concrete operation alone decides whether a fact is terminal or is
safe to Retry. `Failure` never contains credentials, authorization headers,
provider bodies, or arbitrary metadata.

## Optional Result Attributes and atomic transition

Every generated factory accepts an optional exact typed Attribute:

```go
dex.Attribute[sdkgo.QueryResult[OUT]]
dex.Attribute[sdkgo.MutationResult[OUT]]
```

The factory writes the complete Result before choosing its branch. The
Attribute write and the branch decision, including a failure for an unwired
optional branch, are returned in one Dex Execute response and commit together.
If a post-mutation Attribute write must be retried, the same Step execution
retains its Call ID and idempotency key.

Applications explicitly register every configured Attribute and Stream in
`GetPersistenceSchema`. Connector Steps use the application-provided typed
handles directly. The SDK does not aggregate or automatically register
persistence resources. A missing registration fails when the Flow attempts to
write the resource.

## Step defaults and overrides

Manifest execution defaults generate `StepDefaults`: Execute timeout,
optional heartbeat timeout, retry policy, and durability. HTTP defaults are
30 seconds and a five-attempt/two-minute window. OpenAI response creation
defaults are 150 seconds and a five-attempt/five-minute window. Execute
durability is asynchronous unless the operation is very likely to run longer
than seven seconds. Response creation uses synchronous durability. A response
retrieval uses the asynchronous HTTP defaults. GitHub repository change
queries use a five-attempt/65-minute window, because Dex fails a Step whose
provider delay does not fit in the remaining window and GitHub's primary rate
limit resets hourly. Gemini content generation is a synchronous Query with a
300-second Execute timeout, a matching heartbeat timeout because the call does
not stream, and a five-attempt/ten-minute window.

`StepOptionsOverride` overlays non-zero Execute fields and can add
`dex.ProceedToOnExecuteFailure`. That recovery target accepts the original
`STEP_IN`, whereas Connector branch targets accept the factory output
envelope. Execute-only factories reject every WaitFor-specific option.

## Call ID and provider idempotency

Call ID is UUIDv5/SHA-1 over length-prefixed UTF-8 fields in this order:

1. Flow ID;
2. Step execution ID;
3. Connector ID;
4. Operation ID.

The namespace is UUIDv5 of the URL namespace and
`https://superdurable.dev/dex-connectors/call-id/v1`. Run ID, attempt, Worker,
time, randomness, and connection are excluded. Retries and Worker restart in
the same Step execution retain identity; a new Step execution gets a new ID.
This algorithm is a compatibility contract.

Every Mutation derives `IdempotencyKey` from Call ID and input before
credential resolution or dispatch. An empty derivation falls back to Call ID.
Provider adaptation may change namespace, length, or alphabet but cannot use
attempt, Run ID, Worker, time, or randomness. The key covers retries of one
Step execution, not cross-Flow business deduplication.

## Streams

The manifest declares `structured` and/or `text` progress only to decide which
typed Stream fields code generation exposes. Applications define and register
`dex.Stream[sdkgo.ProgressUpdate]` and `dex.Stream[string]`, then pass them to
the generated factory. Runtime operation definitions do not duplicate progress
capability metadata and generic factories do not validate it.

Structured messages add Call ID, Dex attempt, and a sequence starting at one
per attempt. Text uses `dex.BufferedTextStream` and flushes before the handler
returns. Streams are best-effort and outside the Step commit; they cannot
select a branch or represent authoritative completion. Consumers group retry
duplicates by `CallID + Attempt + Sequence`.

A bounded long response such as OpenAI SSE may remain inside one Execute. A
provider-owned asynchronous job is modeled as separate Steps:

```text
start Mutation -> Timer or webhook Channel -> status Query -> complete
```

## Generated Config and Credentials

`connector.yaml` is the source for `zz_generated_connector.go`. It generates
typed `Config`, connector-specific `Credentials`, defaults, validation,
branch constants, operation definitions, operation-specific factory configs,
Step defaults, and identity constants. CI runs `connectorctl generate --check`
to reject drift.

The provider-neutral SDK and every connector are independently released Go
modules. Connector modules require an already-published SDK release and,
for any connector module they depend on, a released, complete tag. Directory-
prefixed Git tags record published versions. Each manifest declares its next
release version, while generated application APIs remain version-independent.

Non-sensitive serializable fields belong in `Config`. HTTP clients,
transports, clocks, test hooks, and idempotency functions are constructor
options. Secret and OAuth fields belong in generated Credentials and are
resolved through `CredentialProvider[C]`.

`Config` holds every `spec.configuration` field and then every auth method's
configuration field as one flat struct. A method configuration field is always
optional in Go: `Config.Validate` checks its type, such as an enum value or an
absolute URL, but never requires it, because `Config` does not know which
methods the connection selected. Dex Web enforces `required: true` while the
method is selected, and connector code checks the selection before it reads
the field.

A manifest with `auth.methods` generates `Credentials` with the union of the
method credential fields. With the default `selection: single`,
`AuthMethodID string` names the one selected method, and `Validate` checks
only that method's fields. With `selection: multiple`, `AuthMethodIDs []string`
replaces it and lists the selected methods in the order they were added.
`Validate` requires a non-empty list of unique, declared IDs and checks each
selected method's credential fields; `HasAuthMethod` reports whether one
method is selected. The generated code for
[schema/testdata/multiple-auth-selection.yaml](../schema/testdata/multiple-auth-selection.yaml)
is checked in at
[internal/codegen/testdata/multipleauthselection/zz_generated_connector.go](../internal/codegen/testdata/multipleauthselection/zz_generated_connector.go):

```go
type Config struct {
	Model                string           `json:"model,omitempty" yaml:"model,omitempty"`
	OpenAIProjectID      string           `json:"openaiProjectId,omitempty" yaml:"openaiProjectId,omitempty"`
	AnthropicWorkspaceID string           `json:"anthropicWorkspaceId,omitempty" yaml:"anthropicWorkspaceId,omitempty"`
	GeminiAPIVersion     GeminiAPIVersion `json:"geminiApiVersion,omitempty" yaml:"geminiApiVersion,omitempty"`
}

type Credentials struct {
	AuthMethodIDs   []string
	OpenAIAPIKey    sdkgo.SecretString
	AnthropicAPIKey sdkgo.SecretString
	GeminiAPIKey    sdkgo.SecretString
}
```

```go
func (credentials Credentials) HasAuthMethod(id string) bool {
	for _, authMethodID := range credentials.AuthMethodIDs {
		if authMethodID == id {
			return true
		}
	}
	return false
}
```

`SecretString` has private storage, redacted formatting, and rejects JSON,
YAML, and text serialization. Connector-specific credential types prevent a
Google Sheets connection from being accidentally passed to Gmail, even when
both use one Google account.

## Local connection file

The Go SDK package `localconfig` reads the path in
`DEX_CONNECTOR_CONFIG_FILE`. Its schema version is
`connectors.dex.dev/local-connections/v1alpha1`. Each record is keyed by
Connector ID and connection name and binds that name to one exact Connector
module path and version.

Generated operation factory configs expose `ConnectionName` with the
`connector:"connectionName"` tag. An empty value preserves ordinary runtime
execution but prevents Dex Web from offering automatic setup. A non-empty
value must equal the typed Connection's runtime name.

Generated `NewLocalConnection` helpers snapshot non-secret configuration at
application startup and install a credential provider that reopens the file
before each provider call. This makes credential replacement visible to a
running app while keeping configuration changes restart-bound. Credential
wire values are converted to `SecretString` only at this boundary and cannot
be serialized again.

A record's `configuration` object holds spec and method configuration fields
side by side, keyed by field name. Its `credentials` object holds the credential
fields plus the selection, which Dex Web writes: `auth_method` is one method ID
for `selection: single`, and `auth_methods` is an array of method IDs for
`selection: multiple`. Strict decoding rejects the other key. This record comes
from the generated fixture test
[internal/codegen/testdata/multipleauthselection/connector_test.go](../internal/codegen/testdata/multipleauthselection/connector_test.go):

```json
"configuration": {"model": "anthropic/claude-sonnet-5", "anthropicWorkspaceId": "wrkspc_test"},
"credentials": {"auth_methods": ["anthropic"], "anthropic_api_key": "anthropic-test-key"}
```

Dex Web also writes the selection beside the record's other members as
`authMethodId` or `authMethodIds`. `localconfig` accepts record members it does
not model, such as these, and a credential refresh writes them back unchanged.
The file's own members and `schemaVersion` stay strict, and connectors still
decode `configuration` and `credentials` strictly. The Dex compatibility gate
proves this against the files released Dex Web writes; see
[Acceptance](acceptance.md).

Dex Web writes non-secret operation-use values to the sibling
`use-configurations.json` file. `localconfig.LoadFile` snapshots this sidecar at
application startup. Applications load one value with
`LoadOperationConfiguration[T]`, identified by connector ID, connection name,
operation ID, Flow type, and Step type. `ConnectorLoadedConfiguration[T]`
keeps that identity beside the strictly decoded value. A Flow explicitly
captures `Value` in `MapToOperationInput`; retries never reread the sidecar.

The local file may contain several named connections for one Connector. It
must never be persisted into Flow state or copied into logs. OAuth refresh
tokens are outside this alpha contract.

The manifest also carries OAuth endpoints, scopes, PKCE, connection kind,
Studio setup entrypoint, reusable unit catalogs, Host API compatibility,
required backend capabilities, mock scenarios, and icon. Release metadata binds
those declarations to the checked UI tarball digest.

Connector UI executes in an opaque-origin iframe and communicates through the
versioned, nonce-bound Studio Host API. Host API 0.2 selects either the
connection surface or one isolated configuration unit. Messages contain only
safe connection status, provider identity, unit port values, configuration,
a bounded frame height, the host theme (`light` or `dark`), `--studio-*`
theme tokens whose names and values the host and `sdk/react` both validate,
and the host's Studio stylesheet. Connector bundles report content changes with
`connector.frame.resize` so the host can fit the opaque-origin iframe without
reading its DOM. OAuth tokens, API keys, client secrets, and refresh tokens
never enter iframe props, messages, markup, logs, or artifacts.

Dex Web owns how bundles look. A bundle renders only the `studio-*` classes in
`sdk/react`'s `connectorStudioClassNames`, and the ready message's optional
`stylesheet` carries the canonical rules for them. The field is additive, so
the protocol version stays 0.2.0. A bundle installs the stylesheet when it is
at most 65,536 UTF-16 code units, contains no `</`, and has a selector for
every class the bundle knows. Otherwise it keeps the copy of the rules compiled
into it. The host applies stricter authoring rules to its own copy;
`sdk/react/README.md` lists them and the markup each class expects.

The Studio BFF loads artifacts directly from the trusted Connector release;
the Java Control Plane is not on this path. OAuth callback, refresh, revoke,
Picker token, and credential-broker behavior remain owned by SuperVerse.

## Text generation connectors

Every LLM lab connector exposes one uniform operation, `generateText`, so an
application can switch labs by changing only the connection. The Go form is in
`sdkgo/llm`; `sdkgo/README.md` shows how a connector builds it.

### The generateText operation

The manifest declares:

- `name: generateText`, `goName: GenerateText`, `inputType: GenerateTextRequest`,
  `outputType: GenerateTextResponse`;
- `kind: query` and `idempotency: none`, because generation creates no provider
  resource and no lab documents a generation idempotency key, so a repeated
  call only bills the tokens again;
- `durability: sync`, because a generation usually exceeds seven seconds and
  an async fallback attempt would send the call to the provider again;
  `llm.NewTextGenerationQuery` rejects any other durability;
- `progress: [text]`, so the generated factory accepts a text Stream;
- exactly these branches, the same IDs Gemini's `generateContent` uses:

```yaml
branches:
  - {id: generated, goName: Generated, description: The model finished normally and returned text.}
  - {id: truncated, goName: Truncated, description: The model stopped at the output token limit and returned any partial text., optional: true}
  - {id: blocked, goName: Blocked, description: "The provider stopped the response for a content policy, or the model refused.", optional: true}
  - {id: providerRejected, goName: ProviderRejected, description: "The provider conclusively rejected the request, such as invalid credentials, an unknown model, or exhausted quota.", optional: true}
  - {id: invalidResponse, goName: InvalidResponse, description: "The provider returned a malformed, oversized, or unusable response, including structured output that does not match its schema.", optional: true}
  - {id: defect, goName: Defect, description: "Local input, connection configuration, or connector definition is invalid.", optional: true}
```

Descriptions may name the provider; the IDs and optionality may not change.
`llm.TextGenerationBranchDefinitions()` returns the same set, and
`llm.NewTextGenerationQuery` rejects any other set or operation ID.

The hand-written client aliases the contract types,
`type GenerateTextRequest = llm.TextGenerationRequest` and
`type GenerateTextResponse = llm.TextGenerationResponse`, so the generated
`GenerateTextResult` is one Go type across connectors, `llm.TextGenerationResult`.
`GenerateText()` returns the `*llm.TextGenerationQuery`.

The request carries `model`, `instructions`, `messages` (role `user` or
`assistant`, with text), `structuredOutput` (`name`, `description`, and a
JSON Schema), `maxOutputTokens`, `temperature` (nil is never sent), and
`reasoningEffort`. The response carries `text`, `requestedModel`,
`servedModel`, `responseId`, `finishReason` (`stop`, `length`,
`contentPolicy`, or `refusal`), a pattern-bounded `providerFinishReason`, and
`usage` in input, cached-input, output, reasoning, and total tokens. Only the
`generated` and `truncated` Results carry text.

A streaming connector writes each text delta to the Step's text Stream as it
arrives, before the finish reason is known, and a retry does not remove it.
The text Stream is a plain `dex.Stream[string]` without Call ID, attempt, or
sequence, so its consumers cannot group retry duplicates. After a Retry it
can hold the interrupted attempt's partial text followed by the whole text of
the next attempt, and a streamed attempt that selects `blocked` or
`invalidResponse` can leave text there. The Result's `text` is the only
authoritative text; the text Stream is progress.

A request field that the connector's wire format does not declare, or that
the chosen model does not accept, selects `defect` with zero provider requests.
Structured output accepts a portable JSON Schema subset: an object root, every
object with `additionalProperties: false` and every property required, and the
keywords `type`, `properties`, `items`, `enum`, `const`, `description`,
`title`, `minimum`, `maximum`, `minLength`, `maxLength`, `minItems`,
`maxItems`, and `format` (`date-time`, `date`, `time`, `uuid`), at most ten
levels deep. The returned text is validated against the application's original
schema even when a provider enforces less; a mismatch selects
`invalidResponse` and names the JSON pointer, never the value. A nullable node
still applies its `enum` and `const`, so `null` passes only when they allow
it. Every number, in the schema or the returned text, is at most 256
characters with an exponent of magnitude at most 400, so the model cannot make
exact validation expensive.

A 402, or a profile rule for a billing error such as a quota 429, selects
`providerRejected` with `FailureQuotaExhausted`, because waiting does not
restore credit. A lab that reports a content-policy block as an error, such as
Kimi's 400 `content_filter` or Meta's 400 `content_policy_violation`, maps it
with `llm.BlockedOutcome()`, so every lab's content-policy stop lands on
`blocked`. 408, 429, and 5xx other than 501, transport failures, and
interrupted event streams return Retry, honoring `Retry-After` up to one hour.
A 2xx response to a streaming request that is not `text/event-stream`, as
from a gateway that ignores streaming, is decoded as a complete response, so
a finished generation is kept and an error object is classified instead of
being retried.

### Families

- **Chat Completions** labs (xAI, Mistral, DeepSeek, Meta, Qwen, and Kimi) use
  `sdkgo/llm/openaichat` with a declarative `Profile` in the connector's
  `profile.go`. A profile declares the path, credential slot and fixed headers,
  instructions role, token-limit field, reasoning-effort map, temperature
  policy, structured-output mode, streaming, a request-field allowlist for
  strict request schemas, finish-token extensions, error rules, response
  header names, and per-model rules by ID prefix or anchored pattern. The
  wire format sends the model in the body, so it uses the body model-ID rule.
- **Native** APIs (OpenAI Responses, Gemini `generateContent`, Claude Messages,
  and Cohere v2 chat) supply an `llm.WireFormat` struct of functions inside
  their own connector module, including the model-ID rule for where their
  model travels. The pipeline, error table, schema checks, event reader, and
  heartbeat stay shared.

A generation endpoint that stores a resource by default is a Mutation, not
`generateText`. OpenAI Responses and any Responses-compatible endpoint must
send `store: false`, so the call keeps Query semantics and an existing
`createResponse` Mutation stays separate.

### Model precedence and the picker

The request's `model`, with surrounding whitespace trimmed, wins; a blank one
uses the connection's configured `model`, trimmed the same way, which the
connector defaults from its manifest and may leave blank. A blank result, or a
model that fails the connector's model-ID rule, selects `defect` without a
request. Once the model
is valid, `requestedModel` is set on every branch; `servedModel` is the
provider's echo, or empty when the provider reports none. A body model is
1 to 256 bytes of printable ASCII; a path-segment model matches
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` after one leading `models/` is removed.
`sdkgo/llm/llmtest/testdata/model_id_cases.json` pins both rules for the
TypeScript validator, `validateModelIDForRule` in
`@superdurable/dex-connectors-react`, whose tests run every case.

Every lab ships the per-Step picker unit `modelPicker` with one string output
port, `model`. A Flow adds the unit to the Step's `ConfigurationUI` and binds
the port to a JSON Pointer such as `/model` in its own configuration shape.
The application loads the pick once at startup with
`localconfig.LoadOperationConfiguration`, treats
`localconfig.ErrConfigurationNotFound` or an empty `model` as "inherit the
connection model", and sets the pick as the request's `model` in
`MapToOperationInput`.

The unit lists models through a Studio command:

- The command ID is `listModels`, its capability is `<connector>.models-list`,
  and both the unit and `studio.setup` declare that capability.
- It is a read-only `GET` pinned to the provider's list URL, with
  `credential: {field: api_key, scheme: bearer}` unless the provider's native
  list needs the key in a header (see the header-scheme bullet below), so the
  Dex Web broker injects the key and the key never reaches the browser frame.
- A lab whose keys work on only one of several fixed hosts declares one
  command per host, `listModels` for the primary host and one more ID for each
  other host, and the bundle tries them in order with
  `executeFirstAcceptedProviderCommand`. Hosts are never templated from iframe
  parameters.
- A native list that needs the key in a provider header, such as Gemini's
  `x-goog-api-key`, uses `credential: {field: api_key, scheme: header, header: ...}`.
  Dex Web through cli-v0.13.8 rejects that scheme before sending, so the lab
  also declares a bearer command for its OpenAI-compatible list when the
  provider offers one, and the bundle tries the native `listModels` first with
  `executeFirstAcceptedProviderCommand`.
- A list failure falls back to manual model entry.
- The OpenAI, Claude, and Gemini loaders and projections are exported from
  `@superdurable/dex-connectors-react/provider-model-lists` and take the
  command IDs and capability from the bundle, so a connector that declares the
  same provider list request reuses them instead of copying provider
  semantics.

### Liveness and budgets

While an attempt is in flight, the pipeline records a nil heartbeat every 5
seconds and stops before `Invoke` returns, so even Dex's 10-second minimum
`heartbeatTimeout` sees two beats of a silent attempt. `generateText` uses a
`heartbeatTimeout` of 60 seconds whether or not it streams, and
`llm.NewTextGenerationQuery` rejects a non-zero value below 10 seconds or an
Execute timeout that does not exceed the request timeout. The heartbeat proves
that the Worker's attempt is alive, not that the provider is progressing, so
the connector's request timeout, the Execute timeout minus 30 seconds, bounds
a hung provider. A profile may add a stall timeout that returns Retry when no
byte, including a keep-alive comment, arrives for that long.

A nil heartbeat carries no checkpoint, and Dex clears any persisted heartbeat
details when it receives one, so retries never resume from a checkpoint. A
business Step that calls `sdkgo.RunQuery` with `generateText` loses any
heartbeat checkpoint it recorded before the call, and must not combine the two.
A Worker lost mid-exchange, or an application override to async durability
whose call outlasts the local phase, repeats the provider call on the next
attempt, which bills the tokens again.

A streaming `generateText` uses an `executeMethodTimeout` of 900 seconds and a
retry policy of 2 seconds initial interval, coefficient 2, 60 seconds maximum
interval, 4 attempts, and 30 minutes total. A provider documented to queue
for up to 10 minutes may use 1200 seconds with a stall timeout.

### Shared code boundary

The root `sdkgo` package stays provider-neutral. `sdkgo/providerhttp` and the
`sdkgo/llm` packages hold shared behavior, and a subpackage holds a wire format
only when at least two connectors use it. No `sdkgo` package contains a
provider host, model ID, error-code value, or credential; those live in each
connector's manifest and profile. `llm.WireFormat` and `llm.RequestFeatures`
grow only by new fields, so a connector built against an older SDK rejects a
newer request field instead of ignoring it. Under minimal version selection a
released connector can link a newer `sdkgo`, so a family wire format such as
`openaichat` declares a later `RequestFeatures` flag only when a new Profile
field, whose zero value leaves it off, opts in; it never turns a new request
field on for connectors whose Profile never vetted it.

## Query, RPC, and Action

Provider Query reads an external system inside a Step. Dex RPC reads or
changes an existing Flow, accepts events, exposes permissioned Actions, or
schedules Steps. RPC has no Step execution ID; Connector calls from RPC fail
before credential resolution or provider access.

Connector Mutation means a provider write. Dex Action means a permissioned
Flow RPC. Studio provider preview belongs to the SuperVerse Connector Service,
not a Flow RPC.
