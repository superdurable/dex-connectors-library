# Dex-native Connector contract v1alpha1

The Go types in `sdkgo` are the executable form of this contract. G2a uses
Dex Server 0.11.3 and `sdk-go v0.11.3`; it does not change Dex Server or its
database.

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
retrieval uses the asynchronous HTTP defaults.

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
modules. Connector modules pin an already-published SDK version. Directory-
prefixed Git tags record published versions. Each manifest declares its next
release version, while generated application APIs remain version-independent.

Non-sensitive serializable fields belong in `Config`. HTTP clients,
transports, clocks, test hooks, and idempotency functions are constructor
options. Secret and OAuth fields belong in generated Credentials and are
resolved through `CredentialProvider[C]`.

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
safe connection status, provider identity, unit port values, and configuration. OAuth
tokens, API keys, client secrets, and refresh tokens never enter iframe props,
messages, markup, logs, or artifacts.

The Studio BFF loads artifacts directly from the trusted Connector release;
the Java Control Plane is not on this path. OAuth callback, refresh, revoke,
Picker token, and credential-broker behavior remain owned by SuperVerse.

## Query, RPC, and Action

Provider Query reads an external system inside a Step. Dex RPC reads or
changes an existing Flow, accepts events, exposes permissioned Actions, or
schedules Steps. RPC has no Step execution ID; Connector calls from RPC fail
before credential resolution or provider access.

Connector Mutation means a provider write. Dex Action means a permissioned
Flow RPC. Studio provider preview belongs to the SuperVerse Connector Service,
not a Flow RPC.
