# Go Module Versioning and Releases

The repository publishes the Connector Go SDK and each connector as a separate
Go module. A Dex application depends only on the connector modules it uses,
and connector releases do not force unrelated upgrades.

## Module and tag identity

The module directory determines the Git tag prefix:

- `sdkgo` uses `sdkgo/vMAJOR.MINOR.PATCH`.
- `connectors/linkedin` uses `connectors/linkedin/vMAJOR.MINOR.PATCH`.
- `connectors/openai` uses `connectors/openai/vMAJOR.MINOR.PATCH`.
- Company-owned families keep independent modules below one directory, such as
  `connectors/google/gmail` and `connectors/google/spreadsheet`.

Each connector manifest declares `metadata.version`. Changing it to the next
patch, minor, or major version requests an automatic release after merge.
Leaving it unchanged explicitly defers release, even when connector code
changes. Directory-prefixed Git tags record completed releases. Generated Go
application APIs do not expose the release version.

GitHub Release titles are human-readable labels. SDK releases use
`Go SDK vMAJOR.MINOR.PATCH`. Connector releases use the manifest
`metadata.displayName`, such as `Slack vMAJOR.MINOR.PATCH` or
`Google Sheets vMAJOR.MINOR.PATCH`.

## Release order

An SDK API change is merged and released before any connector consumes it. A
later connector PR pins that exact published SDK version. Connector modules may
not use a workspace replacement, pseudo-version, branch, or commit SHA in their
checked-in `go.mod`. The same order applies when a connector requires another
connector module; see [Connector dependencies](#connector-dependencies).

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
the public catalog from the registered manifests. Each catalog entry lists UI
units and Triggers by name and description, and operations by name, kind, and
description, so the directory can show and search supported configuration and
actions without reading provider code.

The `Release Connectors and Catalog` workflow runs automatically after a push
to `main`. It compares every declared manifest version with reachable tags and
publishes only requested versions in parallel. A manual run accepts one
optional registered connector directory for repair; leaving it empty rebuilds
only the catalog and site. A workflow rerun includes published tags created at
the original push commit, retaining its release matrix. The workflow:

1. runs the selected connector's Current compatibility gate against the pinned
   Dex CLI/Web baseline;
2. verifies generated code and the standalone connector with `GOWORK=off`;
3. rejects `replace`, pseudo-version, branch, or SHA dependencies on the SDK
   or another connector, and any other module from this repository;
4. proves the exact SDK tag and every required connector tag are reachable
   from `main` and downloadable with `GOPROXY=direct` against the committed
   `go.sum`, and that each required connector release carries
   `connector-release.complete`;
5. verifies the declared version is the next patch, minor, or major;
6. includes only commits that changed that connector directory;
7. builds and tests an optional Connector Studio UI;
8. uploads `connector-release.json`, optional `connector-ui.tgz`, and digests;
9. verifies the published Go module is downloadable;
10. runs Released compatibility against the new component tag;
11. publishes the directory site, `catalog.yaml`, and `card.png` to GitHub
    Pages when connector releases succeed or none are pending. A failed
    release skips that publish. The site is
    `https://superdurable.github.io/dex-connectors-library/` and the README
    preview is `card.png` beside `catalog.yaml`.

The workflow uploads `connector-release.complete` only after both publication
checks pass. A rerun repairs any release without that marker before publishing
the catalog.

CI also runs Current compatibility on pull requests and `main` using
`.dex-compat-version`. Connector publishing uses the same reviewed baseline. A
daily canary runs Released compatibility for the highest stable component tag
of every current connector while setting `DEX_CLI_VERSION=latest`. Maintainers
bump the baseline in a separate PR after the canary passes. Every mode verifies
the selected release checksum. A failed scheduled canary opens or updates one
`dex-compatibility` issue, and a later successful run closes it.
`DEX_CLI_VERSION` and `CONNECTOR_RELEASE_TAG` provide exact failure reproduction.

The release artifact contains the connector ID, complete versioned manifest,
module path, release version and tag, source SHA, source manifest digest, and
optional Studio UI artifact identity and compatibility metadata.
SuperVerse Catalog consumes that artifact instead of inferring a version from
source files.

Breaking changes use the exact lowercase `(breaking)` marker in a PR title,
PR body, or direct commit message. A v0 breaking release cannot use a patch
bump. At v1 or later, a breaking release requires a major bump and the Go
module path must be migrated before publishing v2 or later.

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

The SDK pin also needs a reachable tag and a direct download. No other module
from this repository may be required at any version: not the root tooling
module, another SDK major path such as `sdkgo/v2`, or an `examples/` module.

The check runs each download inside the connector module, so Go verifies it
against the committed `go.sum`. The check never edits `go.sum`. When a
download would add an entry, the check restores the file and fails. Run
`go mod tidy` in the connector and commit the result.

`make test-connectors` runs this check for each affected connector on pull
requests and `main` and for every connector on the scheduled run. The release
job runs it again before publishing. Release the dependency first:
merge its PR, wait for `connector-release.complete`, then pin that exact tag
in a later PR for the dependent connector. Until then, the dependent PR fails
CI. This also keeps one released module per PR.

Go uses minimal version selection, so the pin is a minimum. An application
that requires `connectors/openai v0.8.0` directly and a connector that pins
`v0.7.0` builds both against `v0.8.0`. The dependent connector therefore runs
code it was not released with. A v0 minor may break its build or change a
default it relies on. Batch dependency upgrades into planned releases of the
dependent connector instead of releasing it after every dependency release.
Upgrade immediately for a security fix.

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

Run the compatibility modes locally:

```bash
make test-dex-compat-current
make test-dex-compat-released
```
