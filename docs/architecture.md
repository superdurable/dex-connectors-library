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
  directory without sharing a module or release.
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
Step types, pure business-to-operation input mapping, every branch target,
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

Gmail is a separate module and OAuth Connection. It requests OIDC identity and
`gmail.send`, sends only from the verified primary address, and never reads the
inbox, sent mail, profile, or aliases. Gmail has no server-side idempotency
guarantee, so ambiguous sends route to recovery and are never automatically
repeated.

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
