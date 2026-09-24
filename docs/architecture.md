# Connector library architecture

## Repository boundaries

- `schema/` owns the strict v1alpha1 manifest model and JSON Schema.
- `internal/codegen/` turns one manifest into connector Go types.
- `sdk/go/` is an independently released module owning branch Attempts, Dex
  identity, generic Step factories, generated-factory markers, typed targets,
  credentials, receipts, idempotency, and Stream options.
- `sdk/react/` owns credential-free connection-status primitives.
- `connectors/` owns independently released provider modules and their source
  manifests. Connectors from one company are grouped below one company
  directory without sharing a module or release.
- `cmd/connectorctl/` validates, generates, checks, and catalogs manifests,
  generates the release dropdown, and creates versioned release artifacts.
- `examples/customer-onboarding/` exercises factories against real Dex.

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
Step types, pure business-to-operation input mapping, every branch target,
Result Attributes, Streams, Execute failure policy, and terminal behavior.
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
- OAuth2/OIDC protocol, endpoints, scopes, PKCE, discovery metadata, and nonce
  requirement when applicable;
- whether each operation requires an authorized connection;
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

The SDK and connectors use independent Go modules and directory-prefixed tags.
An SDK contract change is published first; connector modules then pin that
exact release in later PRs. Git tags, not source manifests, define published
versions.

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

## Google Sheets connector

Google Sheets is an independent module and OAuth Connection. It requests only
`drive.file`, so Studio selects each accessible spreadsheet through Google
Picker. `UpsertRow` queries by a stable key before updating or appending;
duplicate keys are an explicit conflict and ambiguous writes remain uncertain.

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
