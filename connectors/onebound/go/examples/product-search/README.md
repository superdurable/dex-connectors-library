# Onebound product search Flow

This runnable Flow uses the static `onebound` connection to search one page of public 1688 listings. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected. Onebound charges for successful and empty-result API calls; repeated starts can incur repeated charges.

## Strict FDG 2.0 generation

From `connectors/onebound/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/product-search/flow/workflow.go --schema-version 2.0 --json --out ./build/product-search
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override onebound=<artifact-directory>`. In Dex Web **Connections**, create `onebound`. Open the [Onebound API product page](https://open.onebound.cn/help/api/1688.item_search.html), select **立即开通**, then sign in to the [Onebound console](https://console.open.onebound.cn/console/). Enable the 1688 `item_search` API, inspect pricing and frequency, and obtain the matching API key and secret from the account. If the console does not display them, use the support contact on the product page. Enter them in protected `api_key` and `api_secret` fields. The key is included in the signed HTTPS URL; the secret is used only for signing. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/product-search
```

In Dex Web **Start Flow**, choose `OneboundProductSearch`, enter a unique Flow ID, and provide a keyword and optional one-based page number. Zero selects page 1:

```json
{"keyword":"work gloves","pageNumber":1}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic product and no provider credential.
