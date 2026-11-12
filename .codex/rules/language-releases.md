# Python and TypeScript connector releases

`AGENTS.md` owns repository release rules. Keep Python packages in `sdkpython/`
or a connector's `python/` directory, and TypeScript packages in `sdkts/` or a
connector's `typescript/` directory. Their manifest language versions, package
metadata, and tags are independent of Go and each other. Release the matching
SDK first. Use `sdkpython:`, `sdkts:`, or `connector(<slug>):` subjects for
single modules.

A breaking change to only some language packages uses the scoped marker
`(breaking: <language>[, <language>...])`, such as `(breaking: typescript)`;
plain `(breaking)` applies to every package. Markers inside Markdown code are
ignored, and any other spelling outside code fails the release. `AGENTS.md`
owns the full rule.

A Python or TypeScript package lists exactly the operations and Triggers it implements in `spec.languages.<language>`; the catalog and site show per-capability language support.

Every Python and TypeScript connector package must replay Go-verified provider
scenarios from `conformance/connectors/<directory>/scenarios.json`. Cover each
Go-tested operation branch, Retry, uncertainty, request count, and provider
idempotency-key behavior. CI requires fixtures for new or changed Python and
TypeScript packages and any connector with existing fixtures. Existing packages
without fixtures report `missing conformance (legacy)` on Go-only changes.
Porters write scenario inputs, prior state, and provider responses; Go record
mode writes complete requests, exact outputs, production idempotency keys,
and retry decisions. CI verifies a nonzero value for every input field, each
declared branch, separate final-attempt 429 and 5xx Retry scenarios and a
transport failure per operation, uncertainty when declared, SDK-encoded exact
outputs, and exact Go recordings. Pending scenarios are rejected; other recovery paths
remain a review requirement.

The reviewed `conformance/exemptions.yaml` exempts only `connectors/postgresql`,
`connectors/mysql`, `connectors/mongodb`, `connectors/microsoft/sql-server`, and
`connectors/superdurable/email`: their native wire protocols cannot be recorded
by the HTTP replay runner. Adding an exemption requires a tooling PR. For each
exempt Python and TypeScript package, replace replay with unit tests for every
Go-tested branch, retry, uncertain decision, and protocol-error classification,
plus real protocol integration tests in CI service containers. Database ports
use Go connector live-test scenarios and connection variables when available.
MongoDB ports use the CI `mongo:7.0` service through `MONGODB_CONNECTOR_TEST_URI`.
Email ports mirror the Go connector's in-process `internal/mailtest` tests,
including `send_message_test.go` and `search_messages_test.go`; their CI
integration tests use the `EMAIL_CONNECTOR_TEST_*` GreenMail variables.
GreenMail lacks STARTTLS, so protocol unit fakes cover that path.
Developers do not run Docker locally. Python integration tests live in
`python/tests/integration/` and TypeScript integration tests in
`typescript/test/integration.test.ts`.

A connector batch port PR may contain 5-8 connectors of one language when every
package is a first `v0.1.0` release and passes conformance. The coordinator
merges one batch at a time and confirms its Release run created every package's
GitHub release, and published each new npm project, before merging the next
batch. Python first uploads to PyPI may lag behind their GitHub releases because
PyPI limits new-project creation rates. Each Release run starts at most
`GO_RELEASE_LIMIT` (default 15) Go connector releases and defers the rest, with
their language packages, to later runs. Other PRs follow the one released module
rule in `AGENTS.md`. Use `connectors(python):` or `connectors(typescript):`
subjects for batch ports.

The first npm publish uses the scoped CI token; later publishes use trusted
publishing when configured. Keep token rotation and optional trust setup in
`docs/versioning-and-releases.md`.
