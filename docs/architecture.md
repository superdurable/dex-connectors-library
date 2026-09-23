# Connector library architecture

## Repository boundaries

- `schema/` owns the strict v1alpha1 manifest model and JSON Schema.
- `internal/codegen/` turns one manifest into connector Go types.
- `sdk/go/` is an independently released module owning branch Attempts, Dex
  identity, generic Step factories, generated-factory markers, typed targets,
  credentials, receipts, idempotency, and Stream options.
- `sdk/react/` owns credential-free connection-status primitives.
- `connectors/` owns provider adapters and their source manifests. The next
  staged delivery converts each connector into an independently released
  module. Connectors from one company are grouped below one company directory
  without sharing a module or release.
- `cmd/connectorctl/` validates, generates, checks, and catalogs manifests.
- `examples/customer-onboarding/` exercises factories against real Dex.

G2a adds no database schema or migration.

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
Step types, pure business-to-operation input mapping, every branch target,
Result Attributes, Streams, Execute failure policy, and terminal behavior.
Non-connector Steps remain ordinary application Steps.

The trusted `ConnectionRef` is constructor-injected when the Flow registers;
it is not public start input. `StepRef[T]` connects one factory to a later
factory without constructing that factory twice. Dex resolves the registered
target by stable type and input type, so the registered target's options apply.

## Factory data flow

```text
STEP_IN
   |
   +-- BuildInput (pure) --error--> DefectBranch
   |
   +-- validate Dex identity + operation + connection
   +-- derive UUIDv5 CallID
   +-- Mutation derives IdempotencyKey
   +-- configure application Streams
   +-- CredentialProvider[C].Resolve(Call)
   +-- provider request
   +-- strict Attempt
          +-- Retry --------------------> Go error / Dex policy
          +-- Uncertain Mutation ------> UncertainBranch
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

## Generated provider surface

Each manifest supplies:

- Go package and field names;
- non-sensitive configuration fields, defaults, and validation metadata;
- connector-specific secret/OAuth fields and connection kind;
- OAuth endpoints, scopes, and PKCE when applicable;
- operation branches, defect and uncertainty branch identities;
- Result Attribute requirement and Stream capabilities;
- Execute timeout, heartbeat, retry, and durability defaults.
- Go operation input/output types used to generate its public Step factory.

`connectorctl generate` writes `zz_generated_connector.go`, including one
strongly typed config and constructor per operation. Generated configs embed a
canonical SDK marker and use metadata tags for branches and resources so Dex
CLI can identify factories by Go type identity instead of connector-specific
function names. Provider code contains only transport and classification
logic. Constructor options carry code dependencies such as HTTP clients and
idempotency derivation functions.

The SDK now uses its own Go module and directory-prefixed tags. Connector
modules adopt the same model in the next staged delivery. An SDK contract
change is published first; connector modules then pin that exact release in
later PRs. Git tags, not source manifests, define published versions.

## HTTP connector

HTTP Query supports GET/HEAD; Mutation supports POST/PUT/PATCH/DELETE. The
connector enforces an explicit host allowlist, HTTPS outside loopback, bounded
responses, safe persisted response headers, credential-only secret headers,
and a stable provider idempotency key.

Branches are generated as:

- Query: `succeeded`, `failed`, `defect`;
- Mutation: `succeeded`, `rejected`, `uncertain`, `defect`.

Query availability and rate limits may Retry. Mutation retries only when the
connector can confirm no provider write occurred. Post-dispatch connection
loss and ambiguous 5xx responses are uncertain.

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

## Static visualization boundary

Runtime factory execution works with Dex Server 0.11.1 and SDK v0.10.2. Dex
CLI 0.11.1 does not yet statically render factory calls in schema 2.0 Flow
Definition. Until the separate Dex CLI patch lands, factory Steps are a known
Dex Web 2.0 visualization limitation. Dynamic Step type, branch collection,
or target construction will remain unsupported even after that patch; use the
canonical static factory form documented in the example.

## UI/UX

G2a changes no Studio or React configuration page. The manifest is UI-ready,
but OAuth callbacks and actual Gmail/Sheets/OpenAI configuration forms belong
to G2c/G6a. Studio must use Streams only for live feedback and Flow
snapshot/Result for authoritative status.
