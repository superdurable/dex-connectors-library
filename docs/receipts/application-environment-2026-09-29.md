# Application environment implementation receipt

## Scope

The isolated Connector SDK source adds declared ordinary application environment
and exact, scope-bound secret references to the shared project configuration
protocol. Native Dex Web owns editing and acceptance. The consuming application
resolves the accepted private versions at startup. No application-specific
fallback, secret default, Flow, database, background refresh, or platform broker
is introduced. No version pin, package publication, image release, or hosted
acceptance is part of this change.

## Durable protocol

The existing configuration schema remains additive at
`connectors.dex.dev/project-configuration/v1alpha1`. The optional `environment`
map contains either `{value: string}` or `{secretRef: {key, version, digest}}`.
It never contains a plaintext value for a declaration marked secret. The secret
object key is exactly `<fixed scope>/app-secrets/<NAME>/<random 32 lowercase hex>`.
Its private JSON binds `{scope, name, value}`; readers verify fixed scope, exact
key format, exact non-null version and `sha256:<hex>` of the full object.

Candidates are created immutably before configuration CAS. A response lost after
storage accepted either write is reconciled by reading the same object identity.
A failed or uncertain CAS cannot justify deleting candidates or old versions.
Accepted immutable snapshots may keep referencing earlier secrets. Scope
cleanup remains the owner of all versions and unreferenced candidates.

All declarations are strings. Canonical declarations include every key `enum`,
`minLength`, `name`, `required`, `secret`; names and unique enums are sorted.
Absent/empty declarations omit `environment` from the semantic environment
contract, preserving existing empty-application digests. Secrets cannot have
an enum or default. Reserved process, SDK, AWS, trust and proxy variables are
rejected. See the owning package README for exact bounds.

## Application boundary

`LoadFromEnvironment` reads the accepted exact configuration. The runnable
`sdkgo/examples/projectconfiguration` then resolves all private versions with
`ResolveApplicationEnvironment` and applies them before constructing application
clients. The wrapper redacts formatting and rejects serialization. Resolution
failure cannot install a partial result. The helper never refreshes credentials
or invokes a provider. Existing empty templates remain free to omit this SDK.

## Tests

The meaningful storage integration is native Dex Web's
`TestProjectApplicationEnvironmentVersionedStorage`, backed by isolated Kind
MinIO with versioning enabled. It exercises safe reads before any Release/FDG,
required-value rejection, an uncertain accepted write, concurrent CAS, private
replacement with preserved old snapshots, restart, stale manifest and configuration
revisions, reserved names, secret classification and cross-scope denial.

Connector SDK tests additionally verify malformed references, envelope scope and
name, exact version/digest, explicit empty-value semantics, canonical declarations,
redaction, and resolving all entries before application. Full owning SDK race
and vet checks run through `make test-common`. Execution results are recorded
in the final work-item receipt after checks finish.

## Limitations

The declared application must adopt the startup helper; legacy code that reads
process environment without it does not gain implicit secret injection. The
example does not prove cloud IAM/KMS policy or a complete application browser
journey. SDK and Dex source remain unpublished, so package pins and developer
skill baselines must wait for approved releases.

## Observed local results

On 2026-09-29, `make test-common` passed SDK race/vet checks, 88 repository
Python checks, and root Go race/vet checks. `make test-projectconfig-integration
test-projectconfig-dex-integration` passed against the existing isolated Kind
MinIO and Dex fixtures. The real Dex execution was
`sdk-project-configuration-1790734057076015000`, RunID
`01a0f011-3e89-7aad-95f6-8d4601aeae16`; credential revision was 3 and refresh was
dispatched once. The temporary S3 bucket was removed by test cleanup.

The corresponding Dex native environment integration passed both exact-version
startup and a manifest change injected during configuration CAS. The latter
returned revision conflict, hid the newly private field, and rejected readiness.
An actual Chrome test served the built native page with an explicit authenticated
embedding fixture and real versioned S3: save, reload, ordinary value retention,
blank private input, secret-safe response, and internal readiness all passed.
This browser fixture is not a complete Superverse BFF or application E2E.

Private local logs: `/private/tmp/sv2-app-environment-sdk-check.log`,
`/private/tmp/sv2-app-environment-sdk-integration.log`,
`/private/tmp/sv2-app-environment-dex-integration.log`, and
`/private/tmp/sv2-app-environment-browser.log`. These logs contain fixture
identities, not production credentials.
