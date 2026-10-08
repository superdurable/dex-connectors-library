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
method the connection selected. Dex Web enforces `required: true` while the
method is selected, and connector code checks the selection before it reads
the field.

A manifest with `auth.methods` generates `Credentials` with the union of the
method credential fields. Each connection selects exactly one method:
`AuthMethodID string` names it, and `Validate` checks only that method's
fields. The generated code for
[schema/testdata/api-key-methods.yaml](../schema/testdata/api-key-methods.yaml)
is checked in at
[internal/codegen/testdata/apikeymethods/zz_generated_connector.go](../internal/codegen/testdata/apikeymethods/zz_generated_connector.go):

```go
type Config struct {
	Model                string           `json:"model,omitempty" yaml:"model,omitempty"`
	OpenAIProjectID      string           `json:"openaiProjectId,omitempty" yaml:"openaiProjectId,omitempty"`
	AnthropicWorkspaceID string           `json:"anthropicWorkspaceId,omitempty" yaml:"anthropicWorkspaceId,omitempty"`
	GeminiAPIVersion     GeminiAPIVersion `json:"geminiApiVersion,omitempty" yaml:"geminiApiVersion,omitempty"`
}

type Credentials struct {
	AuthMethodID    string
	OpenAIAPIKey    sdkgo.SecretString
	AnthropicAPIKey sdkgo.SecretString
	GeminiAPIKey    sdkgo.SecretString
}
```

```go
func (credentials Credentials) Validate() error {
	switch credentials.AuthMethodID {
	case "openai":
		if credentials.OpenAIAPIKey.Reveal() == "" {
			return fmt.Errorf("credential openai_api_key is required")
		}
	case "anthropic":
		if credentials.AnthropicAPIKey.Reveal() == "" {
			return fmt.Errorf("credential anthropic_api_key is required")
		}
	case "gemini":
		if credentials.GeminiAPIKey.Reveal() == "" {
			return fmt.Errorf("credential gemini_api_key is required")
		}
	default:
		return fmt.Errorf("credential auth_method is invalid")
	}
	return nil
}
```

`SecretString` has private storage, redacted formatting, and rejects JSON,
YAML, and text serialization. Connector-specific credential types prevent a
Google Sheets connection from being accidentally passed to Gmail, even when
both use one Google account.

## Project connections

Applications have one way to reach a connection. At startup they call
`projectconfig.LoadFromEnvironment`, which reads the `DEX_PROJECT_*`
deployment contract and the pinned, secret-free configuration snapshot, and
then open every connection that `dex-app.yaml` declares with the connector's
generated `NewProjectConnection(project, connectionName)`. A connector with
Triggers provides one project runner, such as Gmail's
`NewProjectMessageTriggerRunner` or Typeform's
`NewProjectResponseSubmittedEndpointRunner`. It loads each route's stored
binding configuration, fails when a binding is missing, and keeps acknowledged
events in the binding's durable project inbox until its target consumes them.
Superverse Studio, or Dex Web for local development on a local
S3-compatible store, writes the configuration and credentials; an application
never writes them. See [`sdkgo/projectconfig`](../sdkgo/projectconfig/README.md)
for the storage contract and the environment variables.

`NewProjectConnection` decodes the connection's non-secret settings once and
binds a credential provider that resolves credentials from project storage
during each call, so credential replacement and refresh reach a running
application while settings stay restart-bound. The generated code chooses the
provider from the manifest: `auth.refreshable: true` uses a refreshing provider,
which the connector's `New` requires through the generated `CredentialSource`
type, so wiring a connector without refresh does not compile. Applications never
write credential codecs or call credential provider constructors. Credential
wire values are converted to `SecretString` only at this boundary and cannot be
serialized again.

Generated operation and Trigger factory configs expose `ConnectionName` with
the `connector:"connectionName"` tag. It is required and must equal the typed
Connection's name; a missing or different name fails when the Flow is built.
Studio uses the static name to offer each Step's configuration.

A connection's settings hold spec and method configuration fields side by side,
keyed by field name. Its credentials hold the credential fields and, for a
connector with several authorization methods, the selected `auth_method`.

Studio writes non-secret operation-use values into the same configuration
snapshot. Applications load one value with
`provider.LoadOperationConfiguration[T]`, identified by connector ID,
connection name, operation ID, Flow type, and Step type; an absent value matches
`projectconfig.ErrObjectNotFound`. `ConnectorLoadedConfiguration[T]` keeps that
identity beside the strictly decoded value. A Flow explicitly captures `Value`
in `MapToOperationInput`; retries never reread the configuration.

Generated project Trigger factories wrap the target in the binding's durable
project inbox, so an acknowledged event survives a restart and is delivered by
any replica. Project configuration may contain several named connections for
one connector. Credentials must never be persisted into Flow state or copied
into logs.

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
reading its DOM. Stored OAuth tokens, API keys, client secrets, and refresh
tokens never enter iframe props, messages, markup, logs, or artifacts.

A release whose setup declares the `connection.write` capability renders the
whole connection form in its bundle. The host grants it, renders the bundle in
place of the manifest form, and accepts `connection.save` with the complete
configuration, newly typed credentials, and stored fields to keep. The ready
message lists the names of stored credential fields, never their values. Such
a bundle may also send typed but unsaved credentials with
`provider.command.execute`, so a declared command such as a model list runs
before the first save; the host uses them for that call only. A typed
credential therefore passes through that bundle, which is release code the host
verified by digest. The bundle sends it only in those two commands and keeps it
out of markup, logs, and storage. The host serves such a bundle without remote
images, in addition to blocking every network connection.

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

Text generation has one connector,
[`superdurable/llm`](../connectors/superdurable/llm/README.md). Its
`generateText` Query runs a model of the provider that the connection names,
so an application switches providers by changing the connection, not its
code. The Anthropic, DeepSeek, Meta, Mistral, Moonshot Kimi, Alibaba Qwen, and
xAI connectors were removed; each of those providers is a `provider` value of
an `llm` connection. Provider-native operations stay in their own connectors:
OpenAI keeps `createResponse` and `retrieveResponse`, and Gemini keeps
`generateContent`.

The shared request and response types and the pipeline are in
`sdkgo/textgen`, which replaced `sdkgo/llm`. `sdkgo/textgen/textgentest` holds
the conformance suites and the fake provider, and `sdkgo/textgen/openaichat`
holds the Chat Completions wire format. `sdkgo/README.md` shows how a Query is
built.

### The llm connection

One connection names one provider and holds that provider's key:

| Field | Meaning |
| --- | --- |
| `api_key` | The one `apiKey` credential, sent only to the chosen provider's API host. |
| `provider` | Required: `openai`, `anthropic`, `gemini`, `qwen`, `deepseek`, `meta`, `mistral`, `kimi`, or `xai`. |
| `model` | Optional: the model ID as the provider names it, never prefixed with `provider/`. Blank uses the provider's default model. |
| `region` | `global` (the default), `us`, `eu`, `china`, or `hong-kong`. Every provider serves `global`; only Qwen, Mistral, Kimi, and xAI serve another fixed host. |
| `anthropicWorkspaceId` | Only for `anthropic` with a key that spans several workspaces. |
| `maxResponseBytes` | Optional response limit. Blank uses 64 MiB for `deepseek` and 8 MiB for every other provider. |

`llm.New` rejects an unknown provider, a region the provider does not serve, a
model ID the provider's rule rejects, and a workspace ID on another provider
before the Worker starts, without a provider request. Each provider is one row
of the `providerAPIs` table in
[`provider_apis.go`](../connectors/superdurable/llm/provider_apis.go): its
default model, regional base URLs, request timeout, response limit, and wire
format. The wire formats live in the llm module: OpenAI Responses, Claude
Messages, and Gemini `generateContent` natively, and Qwen, DeepSeek, Meta,
Mistral, Kimi, and xAI as `openaichat` profiles. OpenAI requests send
`store: false`, so the call keeps Query semantics.

### The generateText operation

The manifest declares `generateText` with `goName: GenerateText`,
`inputType: GenerateTextRequest`, and `outputType: GenerateTextResponse`, and:

- `kind: query` and `idempotency: none`, because generation creates no
  provider resource and a repeated call only bills the tokens again;
- `durability: sync`, because an async fallback attempt would send the call to
  the provider again;
- `progress: [text]`, so the generated factory accepts a text Stream;
- exactly six branches: `generated`, the only required one, and the optional
  `truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect`.

`textgen.TextGenerationBranchDefinitions()` returns that branch set, and
`textgen.NewTextGenerationQuery` rejects any other branch set, operation ID,
or durability. `llm.GenerateTextRequest` and `llm.GenerateTextResponse` alias
`textgen.TextGenerationRequest` and `textgen.TextGenerationResponse`, so the
Result is `textgen.TextGenerationResult`.

The request carries `model`, `instructions`, `messages` (role `user` or
`assistant`, with text), `structuredOutput` (`name`, `description`, and a JSON
Schema), `maxOutputTokens`, `temperature` (nil is never sent), and
`reasoningEffort`. The response carries `text`, `requestedModel`,
`servedModel`, `responseId`, `finishReason` (`stop`, `length`,
`contentPolicy`, or `refusal`), a pattern-bounded `providerFinishReason`, and
`usage` in input, cached-input, output, reasoning, and total tokens. Only the
`generated` and `truncated` Results carry text.

A request field that the provider's wire format or the chosen model does not
accept selects `defect` with zero provider requests. Structured output accepts
a portable JSON Schema subset: an object root, every object with
`additionalProperties: false` and every property required, the keywords
`type`, `properties`, `items`, `enum`, `const`, `description`, `title`,
`minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, `maxItems`, and
`format` (`date-time`, `date`, `time`, `uuid`), and at most ten levels. The
returned text is validated against the application's original schema even when
a provider enforces less; a mismatch selects `invalidResponse` and names the
JSON pointer, never the value.

A 402, or a billing error such as a quota 429, selects `providerRejected` with
`FailureQuotaExhausted`, because waiting does not restore credit. A
content-policy error, such as Meta's 400 `content_policy_violation`, selects
`blocked` through `textgen.BlockedOutcome()`. 408, 429, and 5xx other than
501, transport failures, and interrupted event streams return Retry, honoring
`Retry-After` up to one hour. A 2xx response to a streaming request that is
not `text/event-stream` is decoded as a complete response.

Streaming providers write each text delta to the Step's text Stream as it
arrives, before the finish reason is known, and a retry does not remove it.
The text Stream is a plain `dex.Stream[string]`, so after a Retry it can hold
the interrupted attempt's text followed by the whole text of the next attempt.
The Result's `text` is the only authoritative text.

### Model precedence and the picker

The request's trimmed `model` wins; a blank one uses the connection's
`model`, and a blank connection model uses the provider's default model. A
model that fails the provider's model-ID rule selects `defect` without a
request. Gemini carries the model in a URL path segment, which must match
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` after one leading `models/` is removed;
every other provider accepts 1 to 256 bytes of printable ASCII without
spaces. `requestedModel` and `Receipt.Provider` identify the model exactly.
[`sdkgo/textgen/textgentest/testdata/model_id_cases.json`](../sdkgo/textgen/textgentest/testdata/model_id_cases.json)
pins both rules for `validateModelIDForRule` in
`@superdurable/dex-connectors-react`.

The connector ships the `modelPicker` unit with one string output port,
`model`. A Flow adds the unit to the Step's `ConfigurationUI` and binds the
port to a JSON Pointer such as `/model`. The application loads the pick once
at startup with `provider.LoadOperationConfiguration`, treats
`projectconfig.ErrObjectNotFound` or an empty pick as the connection's model,
and sets the pick as the request's `model` in `MapToOperationInput`. The
picker reads the connection's saved `provider` and `region` and runs only that
provider's read-only list commands, all under the `llm.models-list`
capability, so the Dex Web broker sends the key only to that provider's hosts
and never to the browser frame. A failed list falls back to model ID entry.

### Liveness and budgets

While an attempt is in flight, the pipeline records a nil heartbeat every 5
seconds, and `generateText` uses a 60-second `heartbeatTimeout`.
`textgen.NewTextGenerationQuery` rejects a non-zero heartbeat timeout below 10
seconds and a non-zero Execute timeout that does not exceed the request
timeout. The heartbeat proves that the Worker's attempt is alive, not that the
provider progresses, so the request timeout bounds a hung provider: 870
seconds, or 1170 seconds for DeepSeek, which queues requests for up to 10
minutes and retries a stream that sends no byte for 5 minutes. The Step
defaults are an `executeMethodTimeout` of 1200 seconds and a retry policy of 2
seconds initial interval, coefficient 2, 60 seconds maximum interval, 4
attempts, and 30 minutes total.

A nil heartbeat carries no checkpoint, and Dex clears any persisted heartbeat
details when it receives one, so a business Step that calls `sdkgo.RunQuery`
with `generateText` must not rely on heartbeat checkpoints. A Worker lost
mid-exchange repeats the provider call on the next attempt, which bills the
tokens again.

### Shared code boundary

The root `sdkgo` package stays provider-neutral. `sdkgo/providerhttp` and the
`sdkgo/textgen` packages hold shared behavior and contain no provider host,
model ID, error-code value, or credential; those live in the llm module.
`textgen.WireFormat` and `textgen.RequestFeatures` grow only by new fields, so
a connector built against an older SDK rejects a newer request field instead
of ignoring it. Under minimal version selection a released connector can link
a newer `sdkgo`, so `openaichat` declares a later `RequestFeatures` flag only
when a new `Profile` field, whose zero value leaves it off, opts in; it never
turns a new request field on for a Profile that never vetted it.

## Query, RPC, and Action

Provider Query reads an external system inside a Step. Dex RPC reads or
changes an existing Flow, accepts events, exposes permissioned Actions, or
schedules Steps. RPC has no Step execution ID; Connector calls from RPC fail
before credential resolution or provider access.

Connector Mutation means a provider write. Dex Action means a permissioned
Flow RPC. Studio provider preview belongs to the SuperVerse Connector Service,
not a Flow RPC.
