# Lingxing authorized Amazon stores Flow

This runnable Flow uses the static `lingxing` connection to list authorized Amazon stores. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/lingxing/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/stores/flow/workflow.go --schema-version 2.0 --json --out ./build/stores
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override lingxing=<artifact-directory>`. In Dex Web **Connections**, create `lingxing`. Sign in to [Lingxing ERP](https://www.lingxing.com/) as a super administrator. Open **Settings > Business Configuration > Global > Open Interface**, enter the notification email, then **Open Backend > API Information**. Copy AppID and AppSecret into the protected `app_id` and `app_secret` fields. Add Dex's outbound public IP to the Open Interface allowlist. See the [official configuration guide](https://apidoc.lingxing.com/new/guides/app-id/). Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/stores
```

In Dex Web **Start Flow**, choose `LingxingStoreList`, enter a unique Flow ID, and use an empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic store and no provider credential.
