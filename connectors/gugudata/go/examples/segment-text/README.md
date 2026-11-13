# GuGuData text segmentation Flow

This runnable Flow uses the static `gugudata` connection to segment one short text. The `segmented` branch stores the result in a Dex Attribute and completes with it. Optional failure branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/gugudata/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/segment-text/flow/workflow.go --schema-version 2.0 --json --out ./build/segment-text
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override gugudata=<artifact-directory>`. In Dex Web **Connections**, create the `gugudata` connection. Sign in at the [developer center](https://www.gugudata.com/developers/portal/appkeys), open **AppKey 与权限**, enable the **NLP 中文文本分词** product, and copy its AppKey into the secret `app_key` field. Leave timeout and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/segment-text
```

In Dex Web **Start Flow**, choose `GugudataSegmentText` and enter a unique Flow ID:

```json
{"text":"计算机科学研究人工智能"}
```

The output includes `segmentedText`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
