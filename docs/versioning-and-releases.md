# Connector Package Versioning and Releases

The repository publishes the Connector Go SDK and each connector as a separate
Go module, and versions and releases each module on its own. A Dex application
depends only on the connector modules it uses, and a change to one connector
releases only that connector.

Python and TypeScript SDKs and connector packages have independent versions.
`sdkpython/` publishes `dex-connectors-sdk` on PyPI; `sdkts/` publishes
`@superdurable/connectors-sdk` on npm. A connector's `python/` and
`typescript/` directories publish `dex-connector-<slug>` and
`@superdurable/connector-<slug>`, where the slug replaces directory separators
below `connectors/` with hyphens. A manifest package override may change the
distribution name.

## Module and tag identity

The module directory determines the Git tag prefix:

- `sdkgo` uses `sdkgo/vMAJOR.MINOR.PATCH`.
- `sdkpython` uses `sdkpython/vMAJOR.MINOR.PATCH`.
- `sdkts` uses `sdkts/vMAJOR.MINOR.PATCH`.
- `connectors/linkedin` uses `connectors/linkedin/vMAJOR.MINOR.PATCH`.
- `connectors/openai` uses `connectors/openai/vMAJOR.MINOR.PATCH`.
- Company-owned families keep independent modules below one directory, such as
  `connectors/google/gmail` and `connectors/google/spreadsheet`.
- Python and TypeScript packages append `/python/vMAJOR.MINOR.PATCH` or
  `/typescript/vMAJOR.MINOR.PATCH` to their connector directory.

Each connector manifest declares that connector's own `metadata.version`. It
is independent of every other connector's version and of the Connector SDK
version. Changing it requests an automatic release of that connector after
merge. Leaving it unchanged explicitly defers release, even when connector code
changes. Directory-prefixed Git tags record completed releases. Generated Go
application APIs do not expose the release version.

`spec.languages.python.version` and `spec.languages.typescript.version` are
separate release sources. Each must match its `pyproject.toml` or `package.json`
version without the leading `v`. A change to one language version releases only
that language. The same patch, minor, major, and `(breaking)` rules apply to
each language independently. Use `sdkpython:` and `sdkts:` commit and PR subjects
for their SDKs; connector changes in any language use `connector(<slug>):`.

GitHub Release titles are human-readable labels. SDK releases use
`Go SDK vMAJOR.MINOR.PATCH`. Connector releases use the manifest
`metadata.displayName`, such as `Slack vMAJOR.MINOR.PATCH` or
`Google Sheets vMAJOR.MINOR.PATCH`.

## Choosing a connector version

A connector pull request that should release moves only that connector's
`metadata.version`, one step from its latest release tag:

- a patch, such as `v0.21.0` to `v0.21.1`, for a fix that adds no public API;
- a minor, such as `v0.21.0` to `v0.22.0`, for an addition such as an
  operation, a Trigger, a UI unit, or mocks;
- for a breaking change, the exact `(breaking)` marker and a minor bump before
  v1, or a major bump at v1 or later;
- for a change that breaks connections, the `(breaking)` marker and a major
  bump, also before v1; see
  [Connection compatibility](#connection-compatibility).

A new connector declares `v0.1.0` for its first release.

Every version that a connector's README files pin for its own module, such as
`go get` commands, example tag URLs, and the `release-artifact` command's
`--version` and `--tag`, stays on the manifest version. A test enforces it.

`connectorctl` and the release workflow reject a version that is not a stable
semantic version or is behind the connector's latest reachable tag, a release
that breaks connections without a major bump, and a `(breaking)` release with
too small a bump. They accept a version that skips a step and a first release
other than `v0.1.0`, so review checks both.

## Connection compatibility

A connection holds a connector's authentication method, its credential, and
its settings. The minor and patch releases of a major version keep every
connection of that major version working, so an application upgrades within a
major version without authorizing again:

- every authentication method keeps its ID, its type, its credential fields
  (name, type, whether each is required, and enum values), and the scopes it
  requests: its OAuth `scopes` and `userScopes`, or the setting that `scopes`
  names with `{fromSetting}`, and the `scopes` of its `clientCredentials` or
  `serviceAccount.jwtBearer` grant from the first release that declares the
  grant;
- every authentication method keeps exactly its host-kept credentials (see
  [Host-kept credentials](manifest-authoring.md#host-kept-credentials)): none
  is added and none is removed, because a host strips them by the latest
  release's declarations and an earlier release of the major version decodes
  the shared credential without exactly those fields;
- `spec.auth.defaultMethod` stays the same, because a credential saved without
  a method uses the default method;
- the settings, `spec.configuration` and each method's `configuration`, only
  gain optional fields: none is removed, renamed, retyped, or made required,
  and no enum value is removed.

A release may add an authentication method. A single `spec.auth` method may
move to `spec.auth.methods` when the `defaultMethod` keeps its contract.
Descriptions, defaults, guides, and endpoints are not part of the connection
contract, and neither are Go names, though renaming one breaks the Go API.

Any other change to the connection contract is a new major version, also
before v1: a v0 connector that breaks its connections releases `v1.0.0`, and
the Go module path changes only from v2. Other breaking changes before v1 keep
the minor bump. Adding an operation or a Trigger stays a minor release as long
as it needs no new OAuth scope. A hosting platform that authorizes connections
with its own registered OAuth application can use a version that adds a scope
only after the provider verifies that application again for the new scope.

The rule holds from the first release of a major version. Until then, the
release candidates of that major version may still change its contract from one
candidate to the next.

`connectorctl` enforces the rule. It compares a manifest with the manifest of
the latest release of the same major version reachable from `main`, and skips
the comparison when that major version has no release yet. `connectorctl release-matrix` applies it to every
pending release, so `make catalog-check` checks a pull request and the release
workflow checks `main` before it publishes anything. `connectorctl
release-candidate` applies it to every candidate. A rejection lists each break
and names the major version the change needs.

The host-kept credentials of a method must not change within a major version
from the first release built with the rule, that is, the first release whose
source contains `schema/host_kept_credentials.go`. `connectorctl` compares them
against every such release. It does not compare them against an earlier
release, and this is not a loosening: an earlier release has no shared
credential decoding, so an application that runs it fails with "this connector
version does not support shared credentials" as soon as a broker strips any
field, whatever a later release declares. Such an application needs the newer
release, and the first release built with the rule establishes each method's
host-kept credentials.

## Dex versions

Dex Server is backward compatible with every earlier Dex SDK release, and the
platform keeps its Dex Server at the newest stable release. The repository
therefore pins no Dex Server, Dex Go SDK, or Dex CLI version:

- A module's `go.mod` records the minimum Dex Go SDK,
  `github.com/superdurable/dex/sdk-go`, that the module needs. Under minimal
  version selection an application may require any newer stable release.
- An application or coding agent may move the Dex Go SDK, the Connector SDK, a
  connector, or the Dex CLI to a newer stable release when useful, after
  reading that release's Breaking Changes section.
- Only released versions are allowed: no prerelease, pseudo-version, or
  `replace` directive.
- CI resolves the latest stable `cli-vX.Y.Z` Dex CLI release and the latest
  stable Dex Go SDK on every run. No checked-in file names a Dex version.

A library module raises its Dex Go SDK minimum only when it needs a newer
release, and then moves to the newest stable release. The exact released
Connector SDK and connector requirements in [Release order](#release-order)
and [Connector dependencies](#connector-dependencies) keep releases
reproducible; they do not cap the versions an application selects.

## Release order

Publish a language SDK first, then update a connector's dependency on its
released version in a later PR. `release-sdk-python.yml` and
`release-sdk-ts.yml` accept a semantic bump, an existing `repair_version`
tag to republish after an interrupted run, and a `dry_run` input. The source
package version must already match the planned bump. Connector language
releases compare each language's manifest version with its own reachable tags.
They build a wheel and source distribution or npm tarball, upload those files,
SHA-256 digests, and `language-release.json` to GitHub Releases, then publish
to PyPI or npm. `dry_run` builds and tests without creating a tag, release,
registry upload, or catalog deployment.

Language builds run with read-only repository access. A separate publish job
checks each package archive's embedded name and version against the release
plan before GitHub or registry upload. Python uses PyPI's existing-file check
on rerun; npm accepts an existing version only when its registry integrity
matches the tarball. Connector language packages require a released SDK, with
no local path, Git, file, workspace, or uv source override. CI and release
builds verify the SDK tag and GitHub release before building the connector.

An SDK API change is merged and released before any connector consumes it. A
later connector pull request requires that exact published SDK version in its
`go.mod` and, when the connector should release, moves its own
`metadata.version`. The two versions are unrelated: a connector at `v0.22.0`
may require `sdkgo v0.24.0` while another connector at `v0.21.0` still
requires `sdkgo v0.21.0`. Connector modules may not use a workspace
replacement, pseudo-version, branch, or commit SHA in their checked-in
`go.mod`. The same order applies when a connector requires another connector
module; see [Connector dependencies](#connector-dependencies).

Every connector requires the same SDK module. Under minimal version selection,
an application that uses several connectors builds all of them against the
highest Connector SDK that any of them requires, so a connector can run with a
newer SDK release than the one it requires. A breaking SDK change can
therefore break an unchanged connector in that application. The
[latest Dex Go SDK check](#latest-dex-go-sdk-check) builds each connector
against the current SDK source and reports that break. Fix it by upgrading and
releasing the broken connector in its own pull request.

The `Release Connector Go SDK` workflow runs only on `main`. It verifies the
standalone module with `GOWORK=off`, finds the latest reachable SDK component
tag, calculates the requested semantic-version bump, and publishes path-scoped
release notes. The first SDK release is `sdkgo/v0.1.0` and must use the default
minor selection.

The root `catalog.yaml` file is a sorted allowlist of connector directories.
Each path starts below `connectors/` and may have any number of directory
levels. The first directory under `connectors/` is the company. Its name is
`metadata.company` with letters and digits lowercased and every other
character removed, and that directory must contain `logo.svg`. CI rejects
missing, unregistered, duplicate, unsafe, or symlinked paths, a missing
company logo, and a company that does not match its directory. It generates
the public catalog from the registered manifests. Entries declaring
`spec.languages` list Go and declared language versions; Go-only entries keep
their earlier format. Each entry lists UI units and Triggers by name and
description, and operations by name, kind, and
description, so the directory can show and search supported configuration and
actions without reading provider code. A connector with mocks also lists them
under `mocks` with the import path of its generated `mockPackage`. The workflow
publishes matching `catalog.yaml` and `catalog.json` documents.

The `Release Connectors and Catalog` workflow runs automatically after a push
to `main`. It compares every declared manifest version with reachable tags and
publishes only requested versions in parallel. A manual run accepts one
optional registered connector directory for repair; leaving it empty rebuilds
only the catalog and site. A workflow rerun includes published tags created at
the original push commit, retaining its release matrix. The workflow:

1. runs the selected connector's Current compatibility gate against the latest
   stable Dex CLI and Web;
2. verifies generated code and the standalone connector with `GOWORK=off`;
3. rejects `replace`, pseudo-version, branch, or SHA dependencies on the SDK
   or another connector, and any other module from this repository;
4. proves the exact SDK tag and every required connector tag are reachable
   from `main` and downloadable with `GOPROXY=direct` against the committed
   `go.sum`, and that each required connector release carries
   `connector-release.complete`;
5. verifies the declared version is later than the connector's latest
   release and keeps the connections of its major version;
6. includes only commits that changed that connector directory;
7. builds and tests an optional Connector Studio UI;
8. uploads `connector-release.json`, optional `connector-ui.tgz`, and digests;
9. verifies the published Go module is downloadable;
10. runs Released compatibility against the new component tag;
11. publishes the directory site, `catalog.yaml`, and `card.png` to GitHub
    Pages when connector releases succeed or none are pending. A failed Go
    or language release blocks the whole catalog publish, including Go entries.
    The site is
    `https://superdurable.github.io/dex-connectors-library/` and the README
    preview is `card.png` beside `catalog.yaml`.

The workflow uploads `connector-release.complete` only after both publication
checks pass. A rerun repairs any release without that marker before publishing
the catalog.

CI also runs Current compatibility on pull requests and `main`. Every mode
resolves the highest stable `cli-vX.Y.Z` Dex release on GitHub, logs its tag
and commit, and verifies the downloaded CLI checksum. It lists the tags with
`git ls-remote` and reads only the chosen release from the REST API, because
every job of a run shares one API rate limit, and it skips a release whose
archive or `checksums.txt` is not uploaded yet. Connector and SDK
publishing resolve the release the same way. A daily canary runs Released
compatibility for the highest stable component tag of every current connector.
The [latest Dex Go SDK check](#latest-dex-go-sdk-check) runs on pull requests,
`main`, and the same daily schedule. A failed scheduled canary opens or updates
one `dex-compatibility` issue, and a later successful run closes it.
`DEX_CLI_VERSION` and `CONNECTOR_RELEASE_TAG` select exact releases only to
reproduce a failure.

The release artifact contains the connector ID, complete versioned manifest,
module path, release version and tag, source SHA, source manifest digest, and
optional Studio UI artifact identity and compatibility metadata.
SuperVerse Catalog consumes that artifact instead of inferring a version from
source files.

The artifact lists every authentication method under `authMethods` with its
`hostKeptCredentials`, an empty array `[]` for a method whose host keeps
nothing. `connectorctl release-artifact` fails the release build when an
entry is missing, has no `hostKeptCredentials`, or has `null` instead of an
array, so every release, candidate, and compatibility build carries the key. A
host fails closed on a release without it.

Breaking changes use the exact lowercase `(breaking)` marker in a PR title,
PR body, or direct commit message. A v0 breaking release cannot use a patch
bump. At v1 or later, a breaking release requires a major bump and the Go
module path must be migrated before publishing v2 or later.

## Release candidates

Python and TypeScript candidates use their language tag prefixes. Their
packages use PyPI `X.Y.ZrcN` and npm `X.Y.Z-rc.N`, published to npm with the
`next` dist-tag. The candidate workflow accepts optional `language` and
`dry_run` inputs. A dry run builds and verifies without publishing. An npm
candidate requires an existing placeholder or stable release, so it cannot
become that package's first `latest` version. Candidates use `NPM_TOKEN`
because trusted publishing is configured for the stable workflow.

A release candidate lets an application use a connector change before review.
It publishes one connector from a commit that is not on `main` yet, tagged
`<connector directory>/vX.Y.Z-rc.N`, where `vX.Y.Z` is the version the
commit's manifest declares and `N` counts from 1. Each new commit of the same
change takes the next `N`. A maintainer still reviews the pull request and the
release workflow publishes `vX.Y.Z` after merge.

The `Release Connector Candidate` workflow runs from `main`. A repository
dispatch of type `connector-release-candidate` with
`{"directory", "commit", "version"}`, or a manual run with the same inputs,
starts it. Because the workflow is read from `main`, a candidate commit cannot
change it. It:

1. checks the request with `connectorctl release-candidate` and `main`'s
   tooling: the commit changes only the connector's directory since it left
   `main` (a new connector also adds its `catalog.yaml` entry and, for a new
   company, its `logo.svg`); its catalog validates; the manifest declares
   `vX.Y.Z`; `vX.Y.Z` is after the connector's latest release reachable from
   `main` and keeps the connections of its major version's latest release; and
   the tag is free or already names the commit;
2. verifies the commit with a read-only token, the only job that runs the
   candidate's code: generated code, the SDK and connector dependency rules,
   `go test -race` and `go vet` with `GOWORK=off`, the Current compatibility
   gate, and an optional Connector Studio UI;
3. builds `connector-release.json` with `main`'s `connectorctl
   release-artifact`, whose version is the candidate version;
4. creates the tag and a GitHub pre-release with the release assets, or repairs
   an existing one at the same commit;
5. verifies the module downloads with `GOPROXY=direct` and uploads
   `connector-release.complete`.

A candidate never enters the public catalog, and no connector may require a
candidate: connector dependencies stay exact releases reachable from `main`.
The Released compatibility canary ignores pre-releases.

## npm token rotation

CI uses npm trusted publishing through GitHub OIDC for packages with a trusted
publisher configured. The first release of a new package uses the repository's
granular `NPM_TOKEN`, with read/write access to `@superdurable`. An existing
package uses the token only when its trusted publisher rejects the workflow.
The token has a 90-day maximum lifetime. The weekly `Check npm publishing
token` workflow runs `npm whoami` and reads repository variable
`NPM_TOKEN_EXPIRES_AT` first. If absent, it matches the current token prefix
in `npm token list`. Unknown expiry produces a warning and a rotation issue.
It fails within 14 days of known expiry or when the token is invalid, and opens or comments on a deduplicated issue mentioning
`@longquanzheng`. npm authentication failures during release do the same.

To rotate the token, create a new granular npm token with read/write access to
the `@superdurable` scope using the owner's account and 2FA. Replace the
repository Actions secret `NPM_TOKEN` and set repository Actions variable
`NPM_TOKEN_EXPIRES_AT` to the new token's ISO expiry date (`YYYY-MM-DD`), then run the `Check npm publishing
token` workflow manually. Do not print or commit the token. After a package's
first publish, the owner may run
`script/npm-configure-trusted-publishers.sh` locally to preview missing
trusted publishers, then add `--apply` with npm account 2FA to configure them.
The script covers this repository's connector packages and TypeScript SDK.

## Latest Dex Go SDK check

`make test-dex-compat-latest-go-sdk` checks that the Connector SDK and the
selected connectors build with the latest stable Dex Go SDK. It resolves that
version once with `go list -m github.com/superdurable/dex/sdk-go@latest`, then
copies `sdkgo` and each connector to a temporary directory. Each connector copy
replaces its released Connector SDK with the copied `sdkgo` source, as an
application does when it moves every library to its newest release. In every
copy the check runs:

1. `go get github.com/superdurable/dex/sdk-go@<latest>` and `go mod tidy`;
2. `go build ./...`;
3. `go vet ./...` with no build tags and with each of `integration` and
   `live`, so tagged test files compile too.

The check only compiles code. It needs no Dex Server or provider credentials
and never edits the checkout. CI runs it for affected connectors on pull
requests and `main` and for every connector on the daily schedule. A failure
names each module that does not build or vet with that Dex Go SDK.

## Connector dependencies

A connector module may require another connector module, for example a
connector that routes each request to one provider connector's Query. Every
`github.com/superdurable/dex-connectors-library/connectors/...` requirement in
a connector's `go.mod`, direct or `// indirect`, must:

- be an exact stable version such as `v0.7.0`, never a `replace`,
  pseudo-version, prerelease, branch, or commit SHA;
- have its tag, such as `connectors/openai/v0.7.0`, reachable from `main`;
- have a GitHub release that carries `connector-release.complete`, so a
  release that failed before its final check does not count;
- download with `GOWORK=off GOPROXY=direct`.

The SDK requirement also needs a reachable tag and a direct download. No other
module from this repository may be required at any version: not the root tooling
module, another SDK major path such as `sdkgo/v2`, or an `examples/` module.

The check runs each download inside the connector module, so Go verifies it
against the committed `go.sum`. The check never edits `go.sum`. When a
download would add an entry, the check restores the file and fails. Run
`go mod tidy` in the connector and commit the result.

`make test-connectors` runs this check for each affected connector on pull
requests and `main` and for every connector on the scheduled run. The release
job runs it again before publishing. Release the dependency first: merge its PR,
wait for `connector-release.complete`, then require that exact tag in a later PR
for the dependent connector. Until then, the dependent PR fails CI. This also
keeps one released module per PR.

Go uses minimal version selection, so each requirement is a minimum. An
application that requires `connectors/openai v0.8.0` directly and a connector
that requires `v0.7.0` builds both against `v0.8.0`. The dependent connector
therefore runs code it was not released with. A v0 minor may break its build or
change a default it relies on. Batch dependency upgrades into planned releases
of the dependent connector instead of releasing it after every dependency
release. Upgrade immediately for a security fix.

CI also runs an advisory job for every connector that requires another
connector. It generates `go.work` with `make workspace` and runs `go vet ./...`
and `go test ./...` in workspace mode, so the dependent builds against the
current source of its dependencies.
A failure warns a dependency PR that it breaks a dependent. It does not block
merge, because that PR cannot change the dependent under the
one-released-module rule.

## Local verification

Run the SDK as a standalone consumer would:

```bash
cd sdkgo
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Run each connector the same way:

```bash
cd connectors/openai
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Validate the catalog source after adding, moving, or deleting a connector:

```bash
go run ./cmd/connectorctl catalog --check --catalog catalog.yaml
```

Generate the public catalog locally:

```bash
go run ./cmd/connectorctl catalog --catalog catalog.yaml --output dist/pages/catalog.yaml
```

Release planning is covered by temporary-repository tests:

```bash
python3 -m unittest script/release/component_release_test.py
```

Validate one connector's SDK and connector dependencies as CI does:

```bash
git fetch origin main --tags
python3 script/release/component_release.py validate-connector \
  --component-path connectors/openai \
  --sdk-module github.com/superdurable/dex-connectors-library/sdkgo
```

The check reads GitHub releases through an authenticated `gh` and compares
tags with `origin/main`. Pass `--main-ref` to compare with another ref. It
never edits the connector's `go.sum`.

Run the compatibility modes locally. Each uses the latest stable Dex release;
set `DEX_CLI_VERSION`, such as `cli-v1.4.2`, only to reproduce a failure:

```bash
make test-dex-compat-current
make test-dex-compat-released
make test-dex-compat-latest-go-sdk
```
