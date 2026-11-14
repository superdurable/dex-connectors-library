# ZenTao product list Flow

This runnable Flow uses the static `zentao` connection to list visible ZenTao products. The `listed` branch stores the typed response in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/zentao/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/product-list/flow/workflow.go --schema-version 2.0 --json --out ./build/product-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override zentao=<artifact-directory>`. In Dex Web **Connections**, create the `zentao` connection. Set `baseUrl` to your HTTPS ZenTao installation root, including its install subpath, as shown in your browser login URL. Following the [REST v1 guide](https://www.zentao.net/book/api/1397.html), POST your `account` and `password` as JSON from a trusted client to `<baseUrl>/api.php/v1/tokens`, then put only the returned token into the secret `token` field. Leave timeout and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/product-list
```

In Dex Web **Start Flow**, choose `ZenTaoProducts`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result contains product metadata only. The mock-backed integration test uses a real Dex Worker and Client with a synthetic product and no provider credential.
