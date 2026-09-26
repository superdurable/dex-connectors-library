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
checked-in `go.mod`.

The `Release Connector Go SDK` workflow runs only on `main`. It verifies the
standalone module with `GOWORK=off`, finds the latest reachable SDK component
tag, calculates the requested semantic-version bump, and publishes path-scoped
release notes. The first SDK release is `sdkgo/v0.1.0` and must use the default
minor selection.

The root `connectors.yaml` file is a sorted allowlist of connector directories.
Each path starts below `connectors/` and may have any number of directory
levels. CI rejects missing, unregistered, duplicate, unsafe, or symlinked
paths. The first directory under `connectors/` is the company. It must match
`metadata.company` and contain `logo.svg`. The catalog lists each connector's
triggers (`name`, `description`) and operations (`name`, `kind`, `description`).

The `Release Connectors and Catalog` workflow runs automatically after a push
to `main`. It compares every declared manifest version with reachable tags and
publishes all requested versions in parallel. A manual run takes no version or
connector input and only retries incomplete releases. The workflow:

1. runs the Current compatibility gate against the pinned Dex CLI/Web baseline;
2. verifies generated code and the standalone connector with `GOWORK=off`;
3. rejects `replace`, pseudo-version, branch, or SHA SDK dependencies;
4. proves the exact SDK tag is reachable and downloadable;
5. verifies the declared version is the next patch, minor, or major;
6. includes only commits that changed that connector directory;
7. builds and tests an optional Connector Studio UI;
8. uploads `connector-release.json`, optional `connector-ui.tgz`, and digests;
9. verifies the published Go module is downloadable;
10. runs Released compatibility against the new component tag;
11. publishes the directory site, `catalog.yaml`, and `card.png` to GitHub Pages
    when releases succeed or none are pending. A failed release skips publishing.

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

Validate the directory registry after adding, moving, or deleting a connector:

```bash
go run ./cmd/connectorctl catalog --check --registry connectors.yaml
```

Generate the public catalog locally:

```bash
go run ./cmd/connectorctl catalog --registry connectors.yaml --output dist/pages/catalog.yaml
```

Release planning is covered by temporary-repository tests:

```bash
python3 -m unittest script/release/component_release_test.py
```

Run the compatibility modes locally:

```bash
make test-dex-compat-current
make test-dex-compat-released
```
