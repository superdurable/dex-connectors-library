# Go Module Versioning and Releases

The repository publishes the Connector Go SDK and each connector as a separate
Go module. A Dex application depends only on the connector modules it uses,
and connector releases do not force unrelated upgrades.

## Module and tag identity

The module directory determines the Git tag prefix:

- `sdkgo` uses `sdkgo/vMAJOR.MINOR.PATCH`.
- `connectors/http` uses `connectors/http/vMAJOR.MINOR.PATCH`.
- `connectors/linkedin` uses `connectors/linkedin/vMAJOR.MINOR.PATCH`.
- `connectors/openai` uses `connectors/openai/vMAJOR.MINOR.PATCH`.
- Company-owned families keep independent modules below one directory, such as
  `connectors/google/gmail` and `connectors/google/spreadsheet`.

Git tags are the only published-version source of truth. Source manifests and
generated Go code do not contain a manually maintained release version.
GitHub Release titles are human-readable labels and do not define module
versions. SDK releases use `Go SDK vMAJOR.MINOR.PATCH`. Connector releases use
the manifest `metadata.displayName`, such as `Slack vMAJOR.MINOR.PATCH` or
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

The generated `Release Connector` workflow adds a static, sorted connector
choice and a `minor|major|patch` choice. `minor` is the default. CI regenerates
the workflow from the connector catalog and rejects drift, so adding a
connector without adding its release choice cannot merge. The workflow:

1. verifies generated code and the standalone connector with `GOWORK=off`;
2. rejects `replace`, pseudo-version, branch, or SHA SDK dependencies;
3. proves the exact SDK tag is reachable and downloadable;
4. derives the next version from the latest reachable component tag;
5. includes only commits that changed that connector directory;
6. builds and tests an optional Connector Studio UI;
7. uploads `connector-release.json`, optional `connector-ui.tgz`, and digests;
8. verifies the published Go module is downloadable.

The release artifact contains the connector ID, complete versionless manifest,
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

Regenerate the release dropdown after adding, moving, or deleting a connector:

```bash
go run ./cmd/connectorctl release-workflow connectors .github/workflows/release-connector.yml
```

Release planning is covered by temporary-repository tests:

```bash
python3 -m unittest script/release/component_release_test.py
```
