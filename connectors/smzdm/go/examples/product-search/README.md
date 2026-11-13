# SMZDM product search Flow

This runnable Flow uses the static `smzdm` connection to search the first 20 product-index matches for a keyword. The `listed` branch stores the typed result in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/smzdm/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/product-search/flow/workflow.go --schema-version 2.0 --json --out ./build/product-search
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override smzdm=<artifact-directory>`. In Dex Web **Connections**, create the `smzdm` connection. Follow the [SMZDM developer onboarding guide](https://openapi.zhidemai.com/pages/rookie/1.%E6%96%B0%E6%89%8B%E6%8C%87%E5%8D%97.html) and email `group-content@zhidemai.com`; the platform provides the App Key and App Secret after onboarding. Enter both in their secret fields. Ask the contact to enable product search and confirm the `request_from` source label. Leave base URL, request source, timeout, and response size blank for their displayed defaults unless SMZDM assigns a different source label.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/product-search
```

In Dex Web **Start Flow**, choose `SMZDMProductSearch`, enter a unique Flow ID, and enter a keyword:

```json
{"keyword":"笔记本"}
```

The result contains titles, prices in yuan, and the total match count. The mock-backed integration test uses a real Dex Worker and Client with a synthetic product and no provider credential.
