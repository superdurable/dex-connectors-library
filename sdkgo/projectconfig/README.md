# Project configuration storage

`projectconfig` shares one versioned object protocol between project Dex Web and
application replicas. Dex Web owns ordinary configuration, API-key entry, and
generic manifest-driven OAuth. The application Connector SDK refreshes a known
expired credential only when an operation uses it. An ordinary entry has no
broker, refresh scheduler, SQL store, or process-local refresh lock. An entry
that names a shared connection instead reads its credential and settings from a
credential broker; see [Shared connections](#shared-connections).

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

`Scope{ProjectID, Kind}` selects one fixed prefix:

- `projects/<projectID>/live`
- `projects/<projectID>/preview`

A Project has one Live and one Preview configuration; Preview is not keyed by a
Session.

The environment-specific bucket and IAM policy provide the outer boundary.
Storage targets and scope must come from trusted deployment configuration, not
browser input. Roles need `GetBucketVersioning`, scoped `GetObject`,
`GetObjectVersion`, `PutObject`, and appropriate KMS encrypt/decrypt permissions.
Deleting objects or versions is reserved for scope cleanup. Retention must not
remove a version referenced by an active configuration or credential head.

`ConfigurationStore` owns one CAS document at `<scope>/configuration/head`.
It validates identity, revision, JSON shape, duplicate bindings, connection
references, and shared connections. Dex Web validates fields against exact connector manifests before
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

## Shared connections

A configuration document at schema version
`connectors.dex.dev/project-configuration/v1alpha3` may declare a credential
broker and name shared connections. Readers also accept `v1alpha2`, which
carries neither; the writer stamps `v1alpha2` on a document that uses neither,
so applications built before `v1alpha3` still read it.

```json
{
  "schemaVersion": "connectors.dex.dev/project-configuration/v1alpha3",
  "connections": [
    {"connectorId": "github", "connectionName": "releases", "modulePath": "...", "provider": "github", "sharedConnectionId": "conn-7", "configuration": {"maxResponseBytes": 8192}}
  ],
  "credentialBroker": {
    "endpoint": "https://broker.example.test/internal/connection-credentials/read",
    "tokenSecretRef": {"key": "projects/p1/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/<32 hex>", "version": "...", "digest": "sha256:..."}
  }
}
```

- `sharedConnectionId` is an entry field. The entry carries no credential. Its
  `configuration` holds the shared connection's current settings, which the
  writer copies into the document like an ordinary entry's, so they are frozen
  with a snapshot. A document with a shared entry must declare `credentialBroker`.
- `credentialBroker.endpoint` is an absolute `https` URL, or `http` for a
  loopback, private, single-label or cluster-local host.
- `credentialBroker.tokenSecretRef` pins the scope token as a private object
  with the application-secret format and the fixed name
  `DEX_CREDENTIAL_BROKER_TOKEN`. The reserved prefix keeps it apart from
  application environment values. `ConfigurationStore.CreateCredentialBrokerToken`
  writes it. The application role needs read access to that object like any
  other application secret, and to `configuration/head` for scope token
  renewal.

### Wire contract

An entry with `sharedConnectionId` reads the broker with one request:

```text
POST <credentialBroker.endpoint>
Authorization: Bearer <scope token>
Content-Type: application/json

{"sharedConnectionId": "conn-7", "rejectedRevision": 3}
```

`rejectedRevision` is present only when a provider rejected that revision. The
broker answers HTTP 200 with a JSON object; unknown members are ignored:

| Member | Meaning |
| --- | --- |
| `status` | `READY`, `REAUTHORIZATION_REQUIRED` or `CONNECTION_NOT_SELECTED`. |
| `credential` | `READY` only. The connection's credential object without its host-kept fields (see [Host-kept secrets](#host-kept-secrets)), at most 64 KiB. |
| `revision` | `READY` only. A positive integer that changes whenever the credential changes. |
| `issuedAt` | `READY`. When the credential was issued, as RFC 3339; absent when unknown. |
| `expiresAt` | `READY`. When it expires, as RFC 3339; absent or `null` for a credential that does not expire. |

HTTP 401 means the broker refused the scope token. The SDK then drops its
cached token, rereads `configuration/head` for the current
`credentialBroker.tokenSecretRef`, reads that secret and, if the token differs,
asks once more. A platform rotates scope tokens by writing a new token secret
with `CreateCredentialBrokerToken` and updating that reference in the head, so
applications need no restart. Without a different token the 401 is a rejection.

HTTP 408, 425, 429 and every 5xx status, and any transport error, are
`ErrCredentialBrokerUnavailable`. Any other status (including a 401 that
renewal does not change), a redirect, a body that is not a JSON object, an
unknown `status` and a `READY` answer without a credential object or revision
are `ErrCredentialBrokerRejected`. A request ends within 40 seconds or when the
calling Step is canceled, and a response is at most 1 MiB.
A broker that has sent a refresh to a provider must finish and store its
result even when the client disconnects, because a lost response can leave a
rotated token unused. Errors never contain the scope token or the response.

A broker that refreshes credentials should use the SDK's rules, so that both
sides agree: `oauthtoken.IsRefreshRequiredWithIssueTime` skews expiry by
`min(5 minutes, lifetime/4)`, and `oauthtoken.ForcedRefreshFloor` is the time
after issue during which a read with `rejectedRevision` returns the credential
unchanged.

### Resolution

`LoadFromEnvironment` does not read the broker, so an application starts while
the broker is unavailable, and only a call's credential read waits for it. The
connector's generated `NewProjectConnection` decodes the entry's settings from
the document like an ordinary entry's. `provider.NewCredentialProvider` and
`NewRefreshingCredentialProvider` route an entry that the document marks as
shared to the broker.

A shared entry then behaves like this:

- It caches the credential in memory and never reads or writes `connections/*`.
- It never calls the connector's refresh driver, not even `RefreshRequired`.
  It reads the broker again when the credential is within `min(5 minutes,
  lifetime/4)` of expiry, where `lifetime` is `expiresAt - issuedAt` (5 minutes
  when `issuedAt` is absent), and a credential without an expiry every 15
  minutes. One read is in flight for an entry; concurrent callers wait for it
  with their own context and use its result.
- The entry remembers the revision it handed to each provider call. The
  provider package passes `Call.ID` through `projectconfig.ContextWithCallID`,
  and the entry keeps at most 4096 calls for 10 minutes. For a call it does not
  remember, it assumes the cached revision. A call keeps its ID across retries,
  so provider requests that one call sends in parallel share one record, which
  holds the revision handed out last; a refresh between them can then cause one
  extra forced read. This is rare and harmless.
- `ResolveAfterRejection` serves the call that a provider rejected. If the
  cache holds another revision than the call received, because a concurrent
  read refreshed it, the call gets the cached credential without a forced read.
  Otherwise it reads again with the call's revision as `rejectedRevision`. If
  the broker returns that revision unchanged, the credential is within its
  forced-refresh floor or cannot be refreshed, so the call fails with
  `ErrRejectedCredentialNotRefreshed` instead of repeating the provider call
  with the rejected credential. A connector that ignores the error returns the
  provider's authentication error, as it does for a refreshed credential that
  is rejected again. The error is never `ErrReauthorizationRequired`: the
  connection stays usable.
- `REAUTHORIZATION_REQUIRED` is `ErrReauthorizationRequired`, which the
  provider package reports as `sdkgo.ErrReauthorizationRequired`.
- `CONNECTION_NOT_SELECTED` fails the call with `ErrConnectionNotSelected`,
  in Live and Preview alike, and nothing is reread. The entry keeps the
  `sharedConnectionId` of the loaded document for the life of the process,
  because its settings came from that document too: following a new selection
  could send one connection's credential to another connection's endpoint. A
  Preview that selects another connection reads it when it starts again with
  the new document.
- A caller that finds a stale credential that has not expired uses it at once
  while another caller's read is in flight. When no credential is cached, or it
  has expired, callers wait for that read with their own context.
- While the broker is unavailable, a cached credential that has not expired
  keeps working, and the broker is not asked again for 10 seconds. An expired
  credential is never returned.
- Callers that arrive within 2 seconds of a failed read share its failure.

A settings change or a new selection reaches an application only through a new
document.

### Host-kept secrets

A host that keeps a shared connection refreshes and mints its credential
itself, from the declarations in the connector's manifest, and it keeps the
secrets that only those token requests use: an OAuth method's client secret and
refresh tokens, a client credentials grant's client secret, and a JWT bearer
grant's key. Applications run code that the host does not trust, so the broker
serves the credential without those fields, and an application can neither
refresh the shared credential nor rotate a single-use refresh token away from
the other applications that share it. Which fields a host keeps follows from
the token grants that each authentication method of the manifest declares.

A connector's generated decoder requires every field, so the resolver decodes a
shared entry's credential with the credential type's
`UnmarshalSharedCredential` method instead, which
`projectconfig.SharedCredentialUnmarshaler` describes, when the type has it. A
connector whose manifest declares host-kept fields implements the method in its
generated code. The method requires the other fields and accepts the host-kept
ones when a broker still sends them. A connector version without the method
decodes a shared credential with its ordinary decoder, which fails with "this
connector version does not support shared credentials" when the broker has
removed host-kept fields. Ordinary entries always use the ordinary decoder.

### Relayed Triggers

A host whose own provider application receives events for many shared
connections, such as one webhook subscription for every installation, can relay
each event to the applications that use the connection. Two calls on
`LoadedProject` serve a connector's relayed receiver, and both keep the scope
token inside this package.

`RegisterRelayedTrigger(ctx, key, registrationPath)` tells the broker that the
shared entry `key` receives relayed events. The connector names the broker's
fixed registration path, and the SDK resolves it against the origin of
`credentialBroker.endpoint`, so the document needs no other field and the
request names no address; the host derives the receiver's URL itself.

```text
POST <origin of credentialBroker.endpoint><registrationPath>
Authorization: Bearer <scope token>
Content-Type: application/json

{"sharedConnectionId": "conn-7", "connectionName": "releases"}
```

The broker answers HTTP 200 with `{"status": ...}`: `REGISTERED` returns nil,
`CONNECTION_NOT_SELECTED` is `ErrConnectionNotSelected`, and `NOT_RELAYED` is
`ErrRelayedTriggerNotRelayed`, for a connection whose events the broker does
not relay. Other statuses and every HTTP status, the 401 renewal, the timeout
and the size limit follow the credential read's [wire contract](#wire-contract),
so `ErrCredentialBrokerUnavailable` is worth retrying and
`ErrCredentialBrokerRejected` is not. A connector registers each time its
relayed receiver starts. An ordinary or unknown entry, and a document without
`credentialBroker`, fail without a request.

`VerifyRelayedRequest(ctx, header, body, receivedAt)` checks that the host
signed a relayed request with the scope token:

```text
Dex-Relay-Timestamp: <Unix seconds>
Dex-Relay-Signature: v1=<hex HMAC-SHA256 of "v1:<timestamp>:<raw body>">
```

The comparison is constant time, and a timestamp more than 5 minutes from
`receivedAt` fails. A failed check is `ErrRelaySignatureInvalid`. A signature
that does not match the cached token rereads the token once, as a 401 does, so
a rotated token keeps working; because the receiver's route is public, such
rereads happen at most once every 30 seconds per project. A token that cannot be
read wraps `ErrCredentialBrokerUnavailable`, which a receiver answers as
retryable. The verifier never calls the broker.

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

## Local development with Dex Web

Under `dexcli dev`, Dex Web stores the connections that a developer configures
in `connections.json` (and operation settings in `use-configurations.json`
beside it), in `$HOME/.dex/connectors` or the `--connector-config-dir`
directory. The application needs no `DEX_PROJECT_*` variables, no S3-compatible
store and no conversion step. It starts with the launch command that Dex Web
shows:

```bash
DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" <your-app-command>
```

`LoadFromEnvironment` then reads those files and serves the same project
protocol as a hosted store, so `NewProjectConnection` and every other
interface of this package behave identically. Setting both
`DEX_CONNECTOR_CONFIG_FILE` and a `DEX_PROJECT_*` variable is an error. Both
files must be regular files with mode `0600`, as Dex Web writes them. This
needs `sdkgo` v0.28.0 or later; connectors need no change.

- Settings, Trigger bindings and operation settings are read once at startup,
  so changing them in Dex Web needs an application restart.
- Credentials are read again before each provider call, so reauthorizing in
  Dex Web takes effect on the next call without a restart.
- Dex Web does not refresh credentials. When a refreshable connector refreshes
  one, the application writes the new credential back to `connections.json`
  while that file still holds the credential it read, so a restart or Dex Web
  keeps the current refresh token. A newer credential that Dex Web saved wins.
- Several processes may share one `connections.json`, such as an application
  Worker and another local tool. They refresh a credential once and adopt each
  other's result, as [Refresh across processes](#refresh-across-processes)
  describes. This needs `sdkgo` v0.35.0 or later in every Go process that
  refreshes the shared connection.
- Trigger inboxes persist in `trigger-inboxes/` beside `connections.json`, so
  acknowledged events survive a restart.
- The connections file holds plaintext secrets for local development. Never
  commit it or use it outside a developer machine.

### Refresh across processes

Each process keeps its own copy of the connection records, so the conditional
writes that admit one refresh coordinate only the goroutines of one process. A
refresh of a local connection therefore also holds a file lock:

1. The caller takes an exclusive `flock(2)` lock on `connections.json.lock`
   beside `connections.json`. The lock file is created with mode `0600`, a
   symbolic link is refused, and it is never renamed or removed. The lock is
   on a separate file because the write-back replaces `connections.json`. It
   covers the whole file, so refreshes of different connections in one file
   run one at a time.
2. Holding the lock, it reads `connections.json` again. When another process
   wrote back a credential that needs no refresh, the caller uses it and sends
   no provider request. This is how a refresh token that the provider rotated
   reaches every process.
3. Otherwise it sends the one refresh request and writes the result back: a
   temporary file in the same directory, `fsync`, rename over
   `connections.json`, then `fsync` of the directory.
4. It releases the lock. An observer that recovers an unfinished refresh of
   its own process holds the lock in the same way.

The wait for the lock ends with the call's context, and after two minutes at
most, with an error that wraps `ErrRefreshFailed`; no provider request was
sent, and the current authorization is kept. When the lock file cannot be
created, for example in a read-only directory, the refresh fails the same way.

Process failures:

- The kernel releases the lock when its holder exits, including a crash or
  `SIGKILL`. The lock descriptor is close-on-exec, so a child process never
  keeps it. The next process then reads the file and refreshes.
- A crash before the rename leaves the complete previous `connections.json`.
  A temporary `.write-*` file may remain beside it; it can hold credentials,
  is readable only by its owner, and can be deleted.
- If the provider rotated the refresh token before the holder died, the new
  token is lost and the file keeps the old one. The next refresh receives
  `invalid_grant`, and the connection requires reauthorization in Dex Web. No
  lock can recover a provider response that died with its process.
- A write-back failure is logged without credential values, and the refresh
  still succeeds in that process. Other processes keep the file's credential;
  with a provider that rotates refresh tokens, their next refresh can then
  require reauthorization.

Limits:

- File locking is implemented on Linux and macOS. On other platforms a
  refresh is coordinated only within one loaded project, the application logs
  a warning when it loads the file, and the file must serve one loaded project
  of one application process.
- Dex Web does not take this lock when it saves a reauthorized credential. A
  reauthorization saved during another process's write-back, which lasts
  milliseconds, can be overwritten; reauthorize again if a call still uses the
  previous authorization.
- The Python and TypeScript SDKs do not take this lock yet. Do not let them
  refresh a connection that a Go process also refreshes from the same file.

Tests load a project from an in-memory `ObjectStore` with
`projectconfigtest.LoadProject(ctx, objects, scope, reference)`, so they can
serve shared entries and relayed Triggers from an in-process broker without
S3. It skips the storage-target checks above, so production code never imports
the `projectconfigtest` package.

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

`make test-common` runs the existing SDK compilation, race and vet checks, which
include the shared connection tests. They run the loaded project against an
in-process HTTP endpoint that plays the broker, so they cover the wire contract
and the resolution rules above but not a real broker.
On Linux and macOS they also start helper processes from the test binary that
share one `connections.json` and refresh against a fake token endpoint that
rotates refresh tokens. They check that concurrent processes send one refresh
request and adopt the rotated token, that a holder killed with `SIGKILL`
releases the lock, and that a token rotated for a killed holder requires
reauthorization.
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
