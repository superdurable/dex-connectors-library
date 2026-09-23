# Go Module Versioning and Releases

The repository publishes the Connector Go SDK and each connector as a separate
Go module. This migration is staged: the SDK module is released first, then
connector modules pin that published SDK version in later PRs. A Dex
application can therefore depend only on the connector modules it uses, and
connector releases do not force unrelated upgrades.

## Module and tag identity

The module directory determines the Git tag prefix:

- `sdk/go` uses `sdk/go/vMAJOR.MINOR.PATCH`.
- `connectors/http` uses `connectors/http/vMAJOR.MINOR.PATCH`.
- `connectors/openai` uses `connectors/openai/vMAJOR.MINOR.PATCH`.
- Company-owned families keep independent modules below one directory, such as
  `connectors/google/gmail` and `connectors/google/spreadsheet`.

Git tags are the only published-version source of truth. Source manifests and
generated Go code do not contain a manually maintained release version.

## Release order

An SDK API change is merged and released before any connector consumes it. A
later connector PR pins that exact published SDK version. Connector modules may
not use a workspace replacement, pseudo-version, branch, or commit SHA in their
checked-in `go.mod`.

The `Release Connector Go SDK` workflow runs only on `main`. It verifies the
standalone module with `GOWORK=off`, finds the latest reachable SDK component
tag, calculates the requested semantic-version bump, and publishes path-scoped
release notes. The first SDK release is `sdk/go/v0.1.0` and must use the default
minor selection.

Breaking changes use the exact lowercase `(breaking)` marker in a PR title,
PR body, or direct commit message. A v0 breaking release cannot use a patch
bump. At v1 or later, a breaking release requires a major bump and the Go
module path must be migrated before publishing v2 or later.

## Local verification

Run the SDK as a standalone consumer would:

```bash
cd sdk/go
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Release planning is covered by temporary-repository tests:

```bash
python3 -m unittest script/release/component_release_test.py
```
