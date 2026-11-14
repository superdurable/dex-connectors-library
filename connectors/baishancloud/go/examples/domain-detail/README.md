# BaishanCloud domain detail Flow

This runnable Flow uses the static `baishancloud` connection to describe one CDN domain. The `found` branch stores the typed response in a Dex Attribute and completes. Optional branches, including `notFound`, are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/baishancloud/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/domain-detail/flow/workflow.go --schema-version 2.0 --json --out ./build/domain-detail
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override baishancloud=<artifact-directory>`. In Dex Web **Connections**, create the `baishancloud` connection. Sign in to the [BaishanCloud CDN portal](https://cdn.portal.baishan.com), request an API token from your account team for Domain Details, and enter it in the secret `token` field without a `token=` prefix. Leave the base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/domain-detail
```

In Dex Web **Start Flow**, choose `BaishanCloudDomainDetail`, enter a unique Flow ID, and use:

```json
{"domain":"cdn.example.com"}
```

The result contains only CDN domain metadata. The mock-backed integration test uses a real Dex Worker and Client with a synthetic domain and no provider credential.
