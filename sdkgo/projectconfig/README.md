# Project configuration storage

`projectconfig` shares one versioned object protocol between project Dex Web and
application replicas. Dex Web owns ordinary configuration, API-key entry, and
generic manifest-driven OAuth. The application Connector SDK refreshes a known
expired credential only when an operation uses it. There is no broker, refresh
scheduler, SQL store, or process-local refresh lock.

The shared package imports only the Go standard library and AWS libraries. Dex
Server/Web imports this package, **not** `projectconfig/provider`. The latter
adapts the protocol to typed Connector SDK interfaces and therefore imports the
Dex Worker SDK. Keeping those imports separate avoids registering two different
copies of the Dex protobuf schema in the Dex Server process.

## Storage and ownership

Construct `NewS3ObjectStore` with authenticated S3 client, fixed bucket/prefix,
and an exact KMS key ARN. The constructor verifies bucket versioning. Reads
require an exact version identity and matching KMS encryption; writes use
`If-None-Match: *` or `If-Match` and disable automatic write retries. An uncertain
write is reconciled by reading the same identity before another mutation.
`AllowUnencrypted` is only for explicitly isolated local MinIO fixtures.

`Scope{ProjectID, Kind, SessionID}` selects one fixed prefix:

- `projects/<projectID>/live`
- `projects/<projectID>/preview/<sessionID>`

The environment-specific bucket and IAM policy provide the outer boundary.
Storage targets and scope must come from trusted deployment configuration, not
browser input. Roles need `GetBucketVersioning`, scoped `GetObject`,
`GetObjectVersion`, `PutObject`, and appropriate KMS encrypt/decrypt permissions.
Deleting objects or versions is reserved for scope cleanup. Retention must not
remove a version referenced by an active configuration or credential head.

`ConfigurationStore` owns one CAS document at `<scope>/configuration/head`.
It validates identity, revision, JSON shape, duplicate bindings, and connection
references. Dex Web validates fields against exact connector manifests before
writing; this store cannot infer whether arbitrary JSON values contain secrets.
Snapshots pin exact version and `sha256:<hex>` digest, and include only ordinary
settings, logical connection identities, and exact private application-secret references. A later connector credential replacement or
refresh does not change the snapshot and does not require rebuilding an app.

`ConnectionStore` owns heads at
`<scope>/connections/<connectorID>/<base64url(connectionName)>/head`.
Private immutable credential/result objects are referenced by exact version and
digest. Safe metadata contains revision, fence, expiry and authorization method.
Credentials are not tied to a connector release: a newer release of the same
connector keeps using them. `ReadConnection` never refreshes. `ReadCredentialMaterial`
is for trusted server-side setup commands and keep-field updates; it must never
be returned to a browser.

Dex Web owns temporary OAuth state using the same `ObjectStore`, beneath the
scope's `oauth/<sha256(state)>` path. It verifies actor, state, callback identity,
expiry, scope, and connector manifest before asking this package to admit an
exchange. Raw state, PKCE, client secret, authorization code, and tokens remain
private encrypted objects, outside Flow input and state.

## Admission, recovery, and refresh

`BeginCredentialExchange` compares the expected revision before provider
exchange. It persists an attempt ID, invocation ID, fence, and bounded deadline.
Only its original winning invocation receives `ProviderDispatchAllowed=true`.
A retry observing the same attempt receives false, including after a lost
response. A conditional-write loser reads the admitted attempt and joins it
without dispatch permission when its identity, base revision and deadline match. Callers must never serialize or reconstruct that permission.

`CommitCredentialExchange` first persists an immutable result, then conditionally
publishes its exact version. `RecoverCredentialExchange` can complete that
handoff after process replacement. It recognizes an already published result by
its active immutable envelope's attempt/fence; a later replacement or revocation
cannot be mistaken for that result. After its deadline, an abandoned
authorization without a result is fenced to `REAUTHORIZATION_REQUIRED`, and an
abandoned refresh restores the prior credential as `READY`. Expiry never grants
a second dispatch for one admission. Late results cannot overwrite a new fence.

`projectconfig/provider` implements the Connector SDK's credential interfaces
for generated connector code; applications never call it. A connector whose
manifest declares `auth.refreshable: true` uses
`NewRefreshingCredentialProvider(store, key, decode, encode)`, and every other
connector uses `NewCredentialProvider(store, key, decode)`. The generated codecs
validate and preserve the complete credential, including a prior refresh token
omitted by a provider.

The provider refreshes before a call when the stored access expiry has
elapsed or the connector's driver requires it: a missing access token (such as
an app-only connection saved without one), an unknown expiry the provider
always sets, or an expiry within the driver's skew. An unclassified HTTP 401
refreshes only when the stored expiry has elapsed. A refresh driver's
reauthorization-required error fences the connection. Any other failure, and an ambiguous or canceled
request, keeps the prior credential `READY` and returns `ErrRefreshFailed`; the
next call refreshes again. Provider callbacks must honor context cancellation;
one request is bounded by 30 seconds. Concurrent callers join via bounded reads
of the admitted result; no background task survives a caller.

A provider may rotate successfully immediately before a process dies without
persisting its result. The package intentionally cannot reconstruct that token:
the prior credential stays current, and the provider's next refusal of the old
refresh token requires reauthorization.

## Declared application environment

AppManifest's optional `application.environment` declares application-owned
strings. Each declaration has `name`, `required`, `secret`, `minLength`, and
`enum`. Names are unique uppercase ASCII identifiers, at most 128 characters;
up to 128 fields are allowed. Values must be valid UTF-8, contain no NUL, and
occupy at most 32,768 bytes. `minLength` counts Unicode code points, not bytes.
An enum contains up to 128 unique values, sorted for its semantic digest. Each
option must meet the field's length constraints. Secret declarations cannot
publish an enum, and no declaration has a default value.

Reserved names include `PATH`, `HOME`, `PORT`, `HOST`, `NODE_OPTIONS`,
`SSL_CERT_FILE`, `SSL_CERT_DIR`, `PUBLIC_BASE_URL`, `HTTP_PROXY`, `HTTPS_PROXY`,
`ALL_PROXY`, `NO_PROXY`, and prefixes `SUPERVERSE_`, `DEX_`, `AWS_`, `LD_`, `GO`,
and `GIT_`. Configuration cannot replace deployment identities, SDK endpoints,
trust roots, process-loader settings, or credential-routing proxies.

`Configuration.Environment` maps names to exactly one `Value *string` or
`SecretRef *ApplicationSecretRef`. An explicit empty ordinary string differs
from an absent value. Secret references pin an object at
`<scope>/app-secrets/<NAME>/<32 lowercase hexadecimal characters>` by exact
nonempty version and `sha256:<hex>` digest. The encrypted private object binds
scope, name, and value. Readers validate all three and the complete object digest.
The browser never receives the private value, reference, or secret digest.

Dex Web validates the accepted AppManifest revision and configuration revision
before saving. It writes immutable secret candidates before the configuration
CAS. Unknown object writes reconcile the same candidate identity; unknown
configuration writes reconcile exact document bytes. A losing or uncertain CAS
does not delete candidates or prior versions: accepted historical snapshots may
still be deployed. Final scope cleanup deletes all versions. Live and Preview
use independent prefixes, and removing Live serving retains accepted values.

`LoadedProject.ResolveApplicationEnvironment` reads all exact secret versions
without provider calls, refresh, writes, or mutable configuration reads. It
returns a private `ApplicationEnvironment` wrapper with `Lookup` and `Apply`.
The wrapper redacts formatting and rejects JSON, text, and YAML serialization.
Resolve everything first, then call `Apply` before reading application settings,
constructing application clients, or starting goroutines. The loader's own S3
bootstrap client uses reserved deployment configuration that cannot be changed
by application environment values. An error returns no partial environment.

Application roles need read-only access to the exact referenced app-secret keys
and versions, alongside their accepted configuration and permitted connection
state. They do not need application-secret write permissions. The private values
must never enter a Flow, API response, log, or generated artifact.

## Application loading

An application calls `LoadFromEnvironment` once at startup, applies the
resolved application environment, and opens every declared connection with the
connector's generated `NewProjectConnection(project, connectionName)`. Each
connector's `examples/` tree is a runnable application that does exactly this.

`LoadFromEnvironment` uses AWS's default rotating credential chain and requires:

| Variable | Meaning |
| --- | --- |
| `DEX_PROJECT_ID` | Fixed project identity. |
| `DEX_PROJECT_SCOPE` | `live` or `preview`. |
| `DEX_PROJECT_SESSION_ID` | Required only for Preview. |
| `DEX_PROJECT_CONFIG_KEY` | Scope's exact `configuration/head` key. |
| `DEX_PROJECT_CONFIG_VERSION` | Exact version frozen by Dex Web. |
| `DEX_PROJECT_CONFIG_DIGEST` | `sha256:<hex>` over exact object bytes. |
| `DEX_PROJECT_STORAGE_BUCKET` | Private versioned project bucket. |
| `DEX_PROJECT_STORAGE_PREFIX` | Optional fixed environment prefix. |
| `DEX_PROJECT_STORAGE_KMS_KEY_ARN` | Exact hosted KMS key ARN; aliases are rejected. |
| `AWS_REGION` | AWS region through the default SDK configuration. |

Local fixtures additionally set `DEX_PROJECT_ALLOW_LOCAL_STORAGE=true` and
`DEX_PROJECT_STORAGE_ENDPOINT` to an explicit local endpoint. The endpoint may
be loopback/private IP, `localhost`, `host.docker.internal`, or a Kubernetes
`.svc.cluster.local` service. This opt-in cannot silently select ordinary AWS
storage without an explicit local endpoint. Hosted deployments omit both.

## Dex Flow Changes

No production Flow types, Steps, RPCs, Attributes, Channels, or Streams change.
Credential material must remain outside all Dex payloads. Existing local and
broker-backed credential providers keep their behavior; applications explicitly
choose the new provider. Connector module release pins are unchanged.

## Database Schema Changes

No SQL schema or migration is introduced. Versioned encrypted objects are the
single credential/configuration owner. Application business facts remain owned
by the consuming application's Flows.

## UI/UX

No React or directory UI is changed by this package. Project Dex Web supplies
the existing connector forms and generic OAuth interface. Its safe connection
view must exclude `CredentialMaterial`, `Object.Contents`, and storage targets.

## Tests

`make test-common` runs the existing SDK compilation, race and vet checks.
`make test-projectconfig-s3-integration` calls real AWS S3/KMS using the caller's
default credential chain. It requires an explicitly configured existing versioned
bucket and exact KMS key ARN. It creates a unique owned prefix, checks conditional
configuration writes, historical snapshot reads, scope isolation, and credential
exchange admission, then deletes only exact object versions under that prefix.
No provider OAuth or refresh is simulated as live verification. The test fails
when its required service or authorization is missing.

Real OAuth/refresh uncertainty and process-death recovery, deployed Pod Identity
negative access, and the full Studio/Worker/provider journey remain separate
acceptance requirements. Storage and compilation results do not establish them.

## Documentation

The owning SDK README links this protocol and the runnable example. The public
API has comments describing credential boundaries, conditional admission, and
unknown outcomes. Changes here are additive. Publish this module before pinning it in Dex Web;
application SDK/connector releases are upgraded only through an explicit consuming
application change.
