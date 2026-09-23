# Connector library architecture

## Repository boundaries

- `schema/` owns the portable manifest schema and strict loader.
- `sdk/go/` owns Dex identity, strict Attempts, credentials, receipts,
  idempotency keys, and Stream options.
- `sdk/react/` owns credential-free connection status UI primitives.
- `connectors/` owns provider adapters and one manifest per adapter.
- `cmd/connectorctl/` validates manifests and emits a deterministic catalog.
- `test/mock-provider/` supplies deterministic provider behavior.
- `examples/customer-onboarding/` proves the contract against a real Dex
  Server.

No database schema or migration is introduced by G2a.

## Dex composition

```text
ReadCustomerProfileStep
  Execute -> RunQuery(http.query)
      SUCCEEDED -> GrantCustomerCreditsStep
      FAILED    -> ProfileReadFailedStep

GrantCustomerCreditsStep
  Execute -> RunMutation(http.mutation)
      SUCCEEDED -> complete
      FAILED    -> CreditGrantFailedStep
      UNKNOWN   -> ReconcileCreditGrantStep

ReconcileCreditGrantStep
  Execute -> RunQuery(http.query) -> complete, retry visibility, or fail
```

Every provider call runs in a concrete Dex Step `Execute`. One Step execution
invokes at most one operation. The application owns stable Step types and
inputs, retry policy, receipt Attributes, recovery, transitions, and terminal
behavior. The SDK does not create generic Query or Mutation Steps.

Customer Onboarding does not expose `ConnectionRef` in its public start input.
Its trusted logical provider connection is fixed by the application when the
Flow is registered and constructor-injected into all provider Steps. The
binding must remain stable while open executions can reference those Steps.

`RunQuery` and `RunMutation` build a Call from `dex.Context`. The Flow ID and
Step execution ID produce stable provider-call identity. An RPC has no Step
execution ID and receives a local failed result before any credential or
provider access.

Provider Query and Dex RPC are different. Provider Query reads an external
system inside a Step. RPC synchronously reads or changes an existing Flow,
receives an Event, exposes a permission-checked Dex Action, or schedules later
work. Studio provider previews use the SuperVerse Connector Service backend
API, not a Flow RPC.

## Operation data flow

```text
dex.Context
   |
   +-- validate FlowID + StepExecutionID + operation + connection
   |
   +-- UUIDv5 CallID
   |
   +-- Mutation.IdempotencyKey(CallID, input)
   |
   +-- configure application Streams
   |
   +-- CredentialProvider.Resolve(Call)
   |
   +-- provider request
   |
   +-- strict Attempt
          +-- Retry --------------------> Go error / Dex retry policy
          +-- Failure or Unknown ------> Result / explicit Flow branch
          +-- Success -----------------> Result / next transition
```

The idempotency key is finalized before credential resolution. Receipt
completion validates Call ID and idempotency key and fills them automatically.

## Progress path

`WithProgressStream` writes typed `ProgressUpdate` messages. Each message has
the stable Call ID, Dex attempt, per-attempt sequence, phase, safe message,
optional fraction, and optional provider sequence. `WithTextStream` creates
one invocation-managed `dex.BufferedTextStream` and preserves output bytes.

The Flow registers both Stream definitions. No library-global Stream exists.
Stream messages are best-effort UI and observability data; they are not part of
the Attribute/transition commit and never determine terminal status.

## HTTP connector

The generic HTTP connector allows `GET`/`HEAD` Queries and
`POST`/`PUT`/`PATCH`/`DELETE` Mutations. Targets stay inside an explicit host
allowlist, non-loopback transport uses HTTPS, response bodies are bounded,
secret request headers come only from `CredentialProvider`, and response
headers are reduced to a safe allowlist before entering Flow state.

The default provider key is Call ID. `Config.IdempotencyKey` can adapt it and
the configured idempotency header receives the final `Call.IdempotencyKey`.
HTTP 429 is a safe Retry. A Mutation connection loss or 5xx after dispatch is
Unknown; Query availability is retryable. Generic Query 404 is Failed. A
domain-specific connector may instead return successful `Found=false`.

Webhook verification covers an HMAC-SHA256 signature over timestamp and body,
a bounded timestamp window, and caller-supplied replay storage.

## OpenAI Responses connector

Non-streaming Create and Retrieve return response ID, model, request ID,
rate-limit headers, output text, and input/cached/output/reasoning/total token
usage.

When either progress option is present, Create sends `stream=true`. The SSE
reader bounds total and per-event bytes, ignores unknown event types, writes
text deltas through the buffered text Stream, and maps created/queued/
in-progress states to structured progress. The final `response.completed`
object is authoritative for value and usage. Event handling follows the
[OpenAI Responses streaming contract](https://platform.openai.com/docs/api-reference/responses-streaming).

`response.failed` and `response.incomplete` are non-retry Failures and retain
known response identity and usage. Early EOF, connection loss, missing terminal
event, or post-dispatch progress failure is Unknown. An explicit
RetrieveResponse Query reconciles a known response ID.

## Manifest and catalog

Operations use only `query` and `mutation`. Idempotency is `none` for Query and
`required` for Mutation. Optional progress capabilities are `structured` and
`text`; the loader validates uniqueness and sorts them for deterministic
catalog output. `action` is reserved for a Dex permissioned Action RPC.

## UI/UX

G2a does not change the React package or SuperVerse Studio. React continues to
normalize credential-safe connection states. A later Studio can inspect
manifest progress capabilities and subscribe to the application Streams, but
must render final state from the Flow snapshot/result rather than Stream data.
