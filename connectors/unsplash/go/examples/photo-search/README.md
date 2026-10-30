# Unsplash photo search example

This runnable Flow searches public Unsplash photos from Dex Web Start Flow. The `searched` branch completes with photo URLs and attribution. Other branches are unwired and fail the Flow. `ConnectionName` is statically `unsplash`. Dex Web summary and display views show the completed provider outcome.

## Generate strict FDG 2.0

From `connectors/unsplash` with the latest stable Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/photo-search/flow/workflow.go --schema-version 2.0 --json --out ./build/photo-search
```

The result must report `valid: true`. Local source may report `connector_release_required` until published.

## Run

From the repository root, create a release override and run Dex:

```bash
mkdir -p /tmp/unsplash-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/unsplash/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/unsplash/go \
  --version v0.2.0 --tag connectors/unsplash/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/unsplash-release/connector-release.json \
  --digest-output /tmp/unsplash-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/unsplash/go/build" --connector-release-override unsplash=/tmp/unsplash-release
```

Set the project environment described by [`projectconfig`](../../../../../sdkgo/projectconfig/README.md), then start the Worker:

```bash
cd connectors/unsplash/go
go run ./examples/photo-search
```

Configure `unsplash / unsplash` in Dex Web Connections using an Access Key from [Your apps](https://unsplash.com/oauth/applications). In Start Flow choose `UnsplashPhotoSearch`, provide a unique Flow ID, and enter:

```json
{"query":"office","perPage":5}
```

`perPage` defaults to ten and accepts 1 through 30. Display returned images using Unsplash-hosted URLs and photographer attribution.

## Tests

`GOWORK=off go test ./...` checks deterministic mapping. CI runs the generated-mock real-Dex test. Live Unsplash behavior remains unverified.
