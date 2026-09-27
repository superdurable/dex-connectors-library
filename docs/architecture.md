# Connector library architecture

## Repository boundaries

- `schema/` owns the strict v1alpha1 manifest model and JSON Schema.
- `internal/codegen/` turns one manifest into connector Go types.
- `sdkgo/` is an independently released module owning branch Attempts, Dex
  identity, generic Step factories, generated-factory markers, typed targets,
  credentials, receipts, idempotency, and Stream options.
- `sdk/react/` owns credential-free connection-status primitives.
- `connectors/` owns independently released provider modules and their source
  manifests. Connectors from one company are grouped below one company
  directory without sharing a module or release. That company directory
  contains `logo.svg`.
- `site/` is the static directory published with the catalog.
- `cmd/connectorctl/` validates, generates, checks, and catalogs manifests,
  plans declared releases, and creates versioned release artifacts.
- provider connector examples exercise factories against real Dex.

The library adds no database schema or migration.

## Dex composition

```text
ReadCustomerProfile factory Query
  succeeded -> GrantCustomerCredits factory Mutation
  failed    -> ProfileReadFailed
  defect    -> ProfileReadFailed

GrantCustomerCredits factory Mutation
  succeeded -> CreditGrantSucceeded
  rejected  -> CreditGrantFailed
  uncertain -> ReconcileCreditGrant factory Query
  defect    -> CreditGrantFailed

ReconcileCreditGrant factory Query
  succeeded -> CreditGrantReconciled
  failed    -> CreditGrantReconcileFailed
  defect    -> CreditGrantReconcileFailed
```

The application writes no provider-specific Step handler. It still owns stable
Step types, pure business-to-operation input mapping, every required branch target,
optional Result Attributes, Streams, Execute failure policy, and terminal behavior.
Non-connector Steps remain ordinary application Steps.

The trusted connector-specific `Connection` is constructor-injected when the
Flow registers; its internal `ConnectionRef` is not public start input.
`StepRef[T]` connects one factory to a later
factory without constructing that factory twice. Dex resolves the registered
target by stable type and input type, so the registered target's options apply.

## Factory data flow

```text
STEP_IN
   |
   +-- MapToOperationInput (pure)
   |
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
   |
   +-- set typed Result Attribute
   +-- one typed dex.GoTo
```

Attribute write and branch transition share the Execute commit. Stream writes
are immediate best-effort messages and remain outside that commit.

`RunQuery` and `RunMutation` implement the provider-call portion and remain
available to advanced concrete Steps. RPC is rejected because it lacks a Step
execution ID.

## Trigger delivery

```text
provider transport (Socket Mode, polling)
   |
   +-- TriggerSource matches and decodes the event
   +-- PrepareTriggerDelivery ---------> local inbox fsync (generated local factories)
   +-- provider acknowledgement
   |
   +-- durable inbox HandleTrigger
          +-- application wrapper (optional)
                 +-- Dex target: filter -> Flow ID -> StartFlow or InvokeRPC
                        +-- Dex Server -> Worker RPC handler
   |
   +-- nil or UndeliverableTriggerError -> remove the event from the inbox
   +-- any other error ------------------> keep the event; retry with backoff
```

The Dex targets classify errors. Only causes that belong to the event are
undeliverable:

- a closed or never-started Flow;
- an RPC handler's own `MarkTriggerUndeliverable` error: a Worker
  `FailedPrecondition` whose detail starts with `Trigger event is
  undeliverable`;
- input that cannot be encoded;
- an empty Flow ID or event ID.

Everything else is retryable, because retrying after a fix can deliver it:

- Dex or Worker unavailability, lock conflicts, and timeouts;
- handler errors, including another Flow's Dex error that a handler returns;
- a Channel message that another update consumed first;
- Flow definition errors, and requests that the Dex Server rejects as invalid,
  such as a Step option below its configured minimum.

The RPC target returns nil for a response it cannot decode, because Dex already
applied the RPC. `sdkgo.DeliverTrigger` retries from 250 milliseconds, doubling
to 30 seconds, and stops waiting as soon as its context ends.

A Trigger runner replays its durable inbox at `Run` start, in order, with the
same backoff. Replay holds the inbox lock for one attempt at a time, so
`PrepareTrigger` never waits for a backoff. Sources still deliver new events
only after replay returns; otherwise a new event could overtake an older
pending one, such as a reply overtaking its pending root. Replay never fails
because of one event; only an unreadable inbox at startup ends `Run` with an
error. Runners built by `sdkgo.NewTrigger` hand their source a target that
treats undeliverable events as consumed.

An RPC target consumes an event whose Flow has not started yet, because it
cannot tell that event from one whose Flow will never exist. A runner that
feeds both a Flow start and that Flow's RPCs must therefore deliver the start
first.

Every stage logs through `log/slog`, to `slog.Default()` unless the
application passes a logger. The component that consumes an event logs its
skip once: the durable inbox or runner for an undeliverable event, and the Dex
target for a filtered one. `DeliverTrigger` logs each backoff and the recovery,
and the inbox logs replay summaries and its own I/O failures. Records carry
IDs and error messages, never payloads. `sdkgo/README.md` lists the messages
and attribute keys.

## Generated provider surface

Each manifest supplies:

- Go package and field names;
- non-sensitive configuration fields, defaults, and validation metadata;
- connector-specific secret/OAuth fields and connection kind;
- OAuth2/OIDC protocol, endpoints, scopes, PKCE, discovery metadata, and nonce
  requirement when applicable;
- whether each operation requires an authorized connection;
- operation branches, the standard defect identity, and optional uncertainty;
- optional typed Result Attributes and generated Stream fields;
- Execute timeout, heartbeat, retry, and durability defaults.
- Go operation input/output types used to generate its public Step factory.

`connectorctl generate` writes `zz_generated_connector.go`, including one
strongly typed config and constructor per operation. Generated configs embed a
canonical SDK marker and use metadata tags for branches and resources so Dex
CLI can identify factories by Go type identity instead of connector-specific
function names. Provider code contains only transport and classification
logic. Constructor options carry code dependencies such as HTTP clients and
idempotency derivation functions.

The SDK and connectors use independent Go modules and directory-prefixed tags.
An SDK contract change is published first; connector modules then pin that
exact release in later PRs. Git tags, not source manifests, define published
versions.

## OpenAI connector

OpenAI Create branches are `completed`, `failed`, `uncertain`, and `defect`;
Retrieve branches are `found`, `failed`, and `defect`. Generated credentials
contain only `SecretString APIKey`.

When a factory supplies progress or text Streams, Create sends `stream=true`.
The bounded SSE reader handles created/queued/in-progress, text delta,
completed, failed, incomplete, and error events while ignoring unknown event
types. The completed Response object is authoritative for value and usage.
Early EOF or a missing terminal event is uncertain and can be reconciled with
RetrieveResponse.

## Gemini connector

Gemini is a separate Google module and API-key Connection. Its
`generateContent` operation calls the stateless Gemini API
`models.generateContent` method, so it is a Query: a lost response retries
instead of selecting uncertainty, and no idempotency key exists. Branches are
`generated`, `truncated`, `blocked`, `providerRejected`, `invalidResponse`,
and `defect`; only `generated` is required. The key travels only in the
`x-goog-api-key` header, and redirects are disabled.

The request maps JSON Schema output to `generationConfig.responseJsonSchema`
and the thinking budget to `thinkingConfig.thinkingBudget`. Responses are
bounded by `maxResponseBytes`; `RetryInfo` or `Retry-After` delays schedule
the Dex retry for 408, 429, and retryable 5xx responses. The call does not
stream, so its heartbeat timeout equals its 300-second synchronous Execute
timeout.

## Meta connector

Meta is a separate module and API-key Connection for Meta Model API. Its
`generateText` operation is the shared text-generation Query: the connector
declares only an `openaichat` Profile for `POST https://api.meta.ai/v1/chat/completions`,
and the `sdkgo/llm` pipeline owns validation, the error table, schema checks,
streaming, and heartbeats. Every request streams, because Meta ends a long
non-streaming request with HTTP 504, so the operation uses the streaming
budget: a 900-second sync Execute, a 60-second heartbeat timeout, and four
attempts over 30 minutes. The key travels only as a bearer token to the one
fixed host; tests reach a loopback fake provider through
`WithBaseURLForTest`.

The Studio bundle's `modelPicker` lists `GET https://api.meta.ai/v1/models`
through the Dex Web broker and hides models outside Muse Spark behind
**Show all models**.

## Claude connector

Claude is a separate module and API-key Connection in the `connectors/anthropic`
company directory, with Go package `claude`. Its `generateText` operation is
the shared text-generation Query: the connector supplies a native
`llm.WireFormat` for `POST https://api.anthropic.com/v1/messages`, and the
`sdkgo/llm` pipeline owns validation, the error table, schema checks,
streaming, and heartbeats. Every request streams, as Claude recommends for
long requests, so the operation uses the streaming budget: a 900-second sync
Execute, a 60-second heartbeat timeout, and four attempts over 30 minutes.
The key travels only as a bearer token to the one fixed host, beside the
required `anthropic-version` header and an optional `anthropic-workspace-id`;
tests reach a loopback fake provider through `WithBaseURLForTest`.

The Studio bundle's `modelPicker` lists `GET https://api.anthropic.com/v1/models`
through the Dex Web broker with a fixed `anthropic-version` header and pages
with `after_id`. Dex Web `cli-v0.13.8` and earlier drop fixed headers, so the
list fails there and the picker offers manual model ID entry.

## xAI Grok connector

Grok is the xAI company's module, in `connectors/xai` with Go package `grok`,
and an API-key Connection. Its `generateText` operation is the shared
text-generation Query: the connector declares only an `openaichat` Profile
for `POST {endpoint}/chat/completions`, and the `sdkgo/llm` pipeline owns
validation, the error table, schema checks, streaming, and heartbeats. The
`endpoint` configuration accepts only `https://api.x.ai/v1` and the US
regional `https://us.api.x.ai/v1`, so the bearer key reaches no other host;
tests reach a loopback fake provider through `WithBaseURLForTest`. Every
request streams, so the operation uses the streaming budget: a 900-second
sync Execute, a 60-second heartbeat timeout, and four attempts over 30
minutes. Model rules follow xAI's per-model reasoning efforts, and error
tokens come from xAI's top-level `code` field.

The Studio bundle's `modelPicker` lists `GET https://api.x.ai/v1/language-models`
through the Dex Web broker, falls back to `GET https://us.api.x.ai/v1/models`,
and hides multi-agent models, which do not serve Chat Completions, and
non-text models behind **Show all models**.

## Mistral connector

Mistral is a separate module and API-key Connection for the Mistral API. Its
`generateText` operation is the shared text-generation Query: the connector
declares only an `openaichat` Profile for `POST {endpoint}/chat/completions`,
and the `sdkgo/llm` pipeline owns validation, the error table, schema checks,
streaming, and heartbeats. The `endpoint` field accepts only the documented
global, EU, and US base URLs, so the key travels only as a bearer token to a
Mistral host; tests reach a loopback fake provider through
`WithBaseURLForTest`. Mistral's request schema rejects unknown fields, so the
Profile's allowlist keeps every body to the fields the wire format needs and
never sends `stream_options`. Every request streams, with the streaming
budget: a 900-second sync Execute, a 60-second heartbeat timeout, and four
attempts over 30 minutes.

The Studio bundle's `modelPicker` lists `GET https://api.mistral.ai/v1/models`
through the Dex Web broker, shows models whose card sets
`capabilities.completion_chat` and is not archived, and hides the rest behind
**Show all models**.

## DeepSeek connector

DeepSeek is a separate module and API-key Connection for the DeepSeek API.
Its `generateText` operation is the shared text-generation Query: the
connector declares only an `openaichat` Profile for
`POST https://api.deepseek.com/chat/completions`, which has no `/v1` segment,
and the `sdkgo/llm` pipeline owns validation, the error table, schema checks,
streaming, and heartbeats. DeepSeek accepts only `json_object` output, so
structured output sends the schema as an instruction and relies on the shared
post-validation. Every request streams, and DeepSeek holds a queued request
open with keep-alive comments for up to 10 minutes before inference starts,
so the operation uses the queued budget: a 1200-second sync Execute, a
60-second heartbeat timeout, four attempts over 30 minutes, and a 5-minute
stall rule. The key travels only as a bearer token to the one fixed host;
tests reach a loopback fake provider through `WithBaseURLForTest`.

The Studio bundle's `modelPicker` lists `GET https://api.deepseek.com/models`
through the Dex Web broker, labels each model with its display name and token
limits, and hides any model whose output modalities omit text behind
**Show all models**.

## GitHub connector

GitHub exposes generated Query factories for authenticated profile plus primary
verified email and for bounded public repositories. Its OAuth manifest requests
only `read:user user:email` with PKCE. The provider implementation never calls a
private-repository endpoint and never returns source, README, events, issues, or
raw provider bodies.

Repository reads default to 100 and cap at 500, use serial pages of at most 100,
deduplicate by provider repository ID, sort by recent push, and mark truncation.
Authentication, scope, verified-email, not-found, terminal provider, retry, and
local-defect paths remain distinct.

## Google Sheets connector

Google Sheets is an independent module and OAuth Connection. It requests only
`drive.file`, so Studio selects each accessible spreadsheet through Google
Picker. `UpsertRow` queries by a stable key before updating or appending;
duplicate keys are an explicit conflict and ambiguous writes remain uncertain.

## Gmail connector

Gmail is a separate module and OAuth Connection. It requests OIDC identity,
`gmail.readonly`, and `gmail.send`. Its Triggers read inbox message metadata,
`GetMessage` reads one message, and sends use only the verified primary
address. It never modifies or deletes messages. Gmail has no server-side
idempotency guarantee, so ambiguous sends route to recovery and are never
automatically repeated.

`NewLocalMessageTriggerRunner` lists every reply route before any root route in
each poll, then delivers every root before any reply, and ends a poll at the
first retryable failure. Every later poll retries that event before any listed
message, even after newer mail pushes it off the polled page. Gmail receives a
thread's root before any reply to it, so the root of every reply the runner
delivers has already started its Flow.

## LinkedIn connector

LinkedIn exposes one generated Query factory for the authenticated member's
OpenID Connect UserInfo claims. Its manifest declares the LinkedIn issuer,
discovery, authorization, token, and UserInfo endpoints; exact
`openid profile email` scopes; PKCE; and mandatory nonce validation. The
Customer Onboarding App owns the callback, token exchange, and ID-token
validation before creating the one-use access-token credential.

The provider implementation calls only UserInfo. It returns bounded `sub`,
name, given/family name, HTTPS picture, locale, and verified email fields. It
does not scrape a LinkedIn profile page or infer employer, title, employment
history, or real-world identity. Missing verified email, insufficient
authorization, revoked authorization, not-found, terminal provider failure,
safe retry, and local defect remain distinct branches or retry behavior.

## Stripe connector

Stripe is an API-key Connector for hosted Checkout Sessions that accept US
bank account payments. `CreateACHCheckoutSession` uses the stable Dex Call ID
as Stripe's idempotency key. A response lost after dispatch or a provider 5xx
is uncertain and must be reconciled with `GetCheckoutSession`; the application
must not create an unrelated replacement charge.

The connector exposes one signed-webhook Trigger for Checkout completion,
delayed-payment success, delayed-payment failure, and expiration. One HTTP
handler verifies the exact bounded request body, prepares every matching event
in a binding-specific durable inbox, queues it for ordered delivery, and only
then acknowledges Stripe. Temporary persistence or queue failures return a
retryable HTTP status; target delivery retries independently. Applications own
event-ID deduplication and treat delayed-payment success, not hosted Checkout
completion alone, as proof of ACH settlement.

Only session IDs, status, currency, amount, expiry, client reference, URL, and
application metadata enter durable state. API keys, webhook secrets, raw
provider bodies, and customer bank or payment-method data stay outside Flow
state, Results, Receipts, Failures, and logs.

## Studio UI distribution

A manifest may declare a setup entrypoint, Host API range, backend capability
IDs, mock scenarios, and icon. The connector release builds a deterministic
`connector-ui.tgz`; its digest and UI contract are recorded in
`connector-release.json`.

Flow Definition identifies the connector and operation. SuperVerse Studio's
BFF downloads only allowlisted release artifacts, verifies and caches them by
digest, then serves the entrypoint in an opaque-origin sandbox iframe. The BFF
does not call the Java Control Plane for artifact loading. The iframe uses a
nonce-bound `postMessage` Host API and never receives provider credentials.

## Static visualization boundary

Factory execution and visualization use Dex Server, CLI, and Go SDK v0.11.3.
Dex CLI recognizes the canonical static factory form documented in the
example. Dynamic Step type, branch collection, or target construction remains
unsupported because it cannot be represented reliably by static analysis.

## UI/UX

The shared G2c contract adds no Studio page. Provider modules can ship
credential-safe setup bundles; SuperVerse G6a owns OAuth callbacks, credential
storage, the Studio BFF loader, and environment bindings. Studio must use
Streams only for live feedback and Flow snapshot/Result for authoritative
status.
