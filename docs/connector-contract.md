# Dex-native Connector contract v1alpha1

The Go types in `sdk/go` are the executable form of this contract. Dex Server
and dexcli stay at 0.11.1 and the library depends on `sdk-go v0.10.2`.

## Operation boundary

A Connector exposes typed `Query[IN, OUT]` and `Mutation[IN, OUT]`
operations. Applications invoke an operation only from a concrete Dex Step
`Execute` through `RunQuery` or `RunMutation`.

One Step execution invokes one Connector operation. A second invocation in the
same Step deliberately reuses the same `CallID`; this is prohibited. Put the
second call in another Step. A loop uses `GoTo` to create the next Step
execution.

Provider Query is not a Dex RPC. RPC has no Step execution ID, provider retry
boundary, or recovery boundary. An empty Flow ID or Step execution ID produces
a `LOCAL_DEFECT` result before credential resolution or a provider request.

## Strict Attempt model

Operation implementations cannot return an unclassified Go error:

```go
type Query[IN, OUT any] interface {
    Definition() QueryDefinition
    Invoke(Call, IN) QueryAttempt[OUT]
}

type Mutation[IN, OUT any] interface {
    Definition() MutationDefinition
    IdempotencyKey(CallID, IN) IdempotencyKey
    Invoke(Call, IN) MutationAttempt[OUT]
}
```

An implementation converts credential, HTTP, encoding, decoding, and provider
errors into one of the SDK constructors before returning:

- Query: `NewQuerySuccess`, `NewQueryFailure`, or `NewQueryRetry`.
- Mutation: `NewMutationSuccess`, `NewMutationFailure`,
  `NewMutationUnknown`, or `NewMutationRetry`.

Attempt fields are private. A zero or invalid Query Attempt becomes
`LOCAL_DEFECT / FAILED`; a zero or invalid Mutation Attempt becomes
`LOCAL_DEFECT / UNKNOWN`. Invalid combinations never reach a provider retry
policy.

`RunQuery` and `RunMutation` return a non-nil Go error only for an explicit
Retry Attempt. A positive provider delay becomes `dex.RetryAfter`; a zero
delay returns `*connector.RetryError` and lets the Step retry policy choose its
backoff. `FAILED` and `UNKNOWN` always return `error == nil`.

The Process must inspect every result outcome. It may transition to a failure
Step, reconcile an unknown write, or force-fail. It must not treat `error ==
nil` as provider success.

## Failure facts and Process policy

`FailureKind` records a fact, not retry policy. The same fact can be a Failure
or a Retry depending on the operation contract. For example, `NOT_FOUND` may
be a successful `Found=false`, a terminal Failure, or a Retry during a known
eventual-consistency window.

| Kind | Fact |
| --- | --- |
| `VALIDATION` | Connector input or request shape is invalid |
| `AUTHENTICATION` | Credentials are missing, expired, or rejected |
| `AUTHORIZATION` | Required provider permission is absent |
| `NOT_FOUND` | The provider object is absent |
| `CONFLICT` | Provider state conflicts with the request |
| `RATE_LIMIT` | Provider explicitly throttled the request |
| `AVAILABILITY` | Provider availability prevented confirmation |
| `PROVIDER_REJECTION` | Provider explicitly rejected or terminated work |
| `TRANSPORT` | Transport failed before an outcome was confirmed |
| `RESPONSE_TOO_LARGE` | A configured response bound was exceeded |
| `PROTOCOL` | Provider response or event shape was unusable |
| `LOCAL_DEFECT` | The local Connector contract was violated |

`Failure` contains only kind, provider, operation, and a safe message. It does
not implement `error` and must never contain a credential, authorization
header, provider body, or arbitrary metadata.

## Mutation outcomes and reconciliation

| Outcome | Meaning | Process policy |
| --- | --- | --- |
| `SUCCEEDED` | Provider confirmed the write | continue or complete |
| `FAILED` | Provider confirmed rejection or the request never dispatched | explicit failure branch |
| `UNKNOWN` | The request may have executed but confirmation was lost | explicit reconciliation Query |

Only a connector that can prove a Mutation was not executed may return Retry.
After dispatch, connection loss, truncated response, or an ambiguous server
error returns Unknown. This prevents the Dex retry policy from blindly
repeating a provider write.

Reconciliation is visible Process behavior. The application defines a
recovery Step and uses a Query operation to inspect the original Call ID,
provider object ID, provider request ID, or deterministic marker. The SDK does
not hide or automatically run reconciliation.

## CallID compatibility contract

The SDK derives `CallID` as UUIDv5/SHA-1 using:

1. namespace `UUIDv5(URL namespace, "https://superdurable.dev/dex-connectors/call-id/v1")`;
2. UTF-8 fields in this order: `FlowID`, `StepExecutionID`, `ConnectorID`,
   `OperationID`;
3. each field prefixed by its unsigned 32-bit big-endian byte length.

Run ID, attempt, Worker identity, current time, randomness, and connection name
are excluded. Dex retries, Worker restart, Flow retry, and time travel into the
same Step execution retain the Call ID. A new Step execution gets a new ID.
Changing this derivation is a compatibility-breaking change.

## Provider IdempotencyKey

Every Mutation derives a provider-specific `IdempotencyKey` from the stable
`CallID` and input before credential resolution or provider dispatch. Returning
an empty key selects `IdempotencyKey(CallID)`. The final key is present on
`Call` and automatically copied to `Receipt`.

An operation may adapt the Call ID to a provider's namespace, character set,
or length, but it must not use attempt, Run ID, Worker identity, time, or
randomness. The key guarantees retry identity only within one Step execution;
it does not provide cross-Flow business deduplication.

All manifest Mutations declare `idempotency: required`. A provider without an
idempotency header must map the key to a deterministic resource ID, request
field, or queryable marker and use it for every write.

## Progress Streams

Applications define and register their own Dex Streams in the Flow
`PersistenceSchema`, then opt in per call:

```go
connector.WithProgressStream(Progress)
connector.WithTextStream(Text, dex.BufferedTextStreamMaxBytes(16<<10))
```

`Call.ReportProgress` writes `ProgressUpdate`; `Call.WriteText` writes through
`dex.BufferedTextStream`. Without the matching option both methods are no-ops,
so one Connector works in Processes with or without live UI.

Structured progress adds `CallID`, Dex attempt, and a sequence starting at one
for each attempt. Retries may duplicate provider progress. Consumers group by
`CallID + Attempt + Sequence`; they do not deduplicate only by sequence.
Buffered text preserves byte order and the Dex invocation finalizer flushes the
tail before the Step result or error is sent.

Stream writes are best-effort, immediately visible, and outside the Step
commit. Connectors do not emit an authoritative `COMPLETED` message. Final
success, failure, unknown outcome, receipts, and transitions come from the
Step result, Attributes, and Flow snapshot. A Stream write is activity, but an
operation that can remain silent longer than its timeout must adjust the Step
timeout or record a real checkpoint instead of manufacturing progress.

If progress delivery fails after a Mutation dispatched, the outcome is
Unknown, not Retry.

## Long connections and provider jobs

A provider connection that continuously yields bounded events may stay inside
one `Execute`; OpenAI Responses SSE is the initial example. A provider job that
runs independently must not keep a Step open indefinitely. Model it as:

```text
StartJob Mutation -> wait with Timer or webhook Channel -> QueryJob -> complete
```

The job ID and start receipt are durable state. Polling and webhook handling
are separate Step executions with their own Call IDs.

## Receipts and credentials

Receipts contain safe correlation data only. Applications persist a Mutation
receipt in an Attribute in the same Dex commit as the next transition.

`ConnectionRef` never contains a secret or OAuth token. A public Flow input
must not accept a caller-selected connection unless the application boundary
has already authorized that exact binding. Customer Onboarding instead fixes a
logical connection when its Flow is registered and injects it into provider
Steps. `CredentialProvider.Resolve(Call)` may authorize against Flow identity,
Step execution identity, connector/operation identity, connection, Call ID,
and idempotency key. Credential values cannot be JSON, text, or YAML serialized
and are redacted by normal Go formatting.

Connector Mutation means a provider write. Dex Action means a permissioned RPC
whose availability can depend on Flow state. The two terms are not aliases.
Hosted credential resolution and provider preview belong to the SuperVerse
Connector Service.
