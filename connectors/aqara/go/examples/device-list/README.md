# Aqara device list Flow

This runnable Flow uses the static `aqara` connection to read one device page. The `listed` branch stores the typed page in a Dex Attribute and completes with it. Optional provider rejection, invalid response, and defect branches are unwired, so they fail the Flow if selected.

## Strict FDG 2.0 generation

From `connectors/aqara/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/device-list/flow/workflow.go --schema-version 2.0 --json --out ./build/device-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override aqara=<artifact-directory>`. In Dex Web **Connections**, create the `aqara` connection. Follow its authorization guide: create an approved project at [Aqara Developer Platform](https://developer.aqara.com), copy AppID, KeyID, and AppKey from the project's details and key management pages, then obtain an accessToken for an authorized account or project. Save the AppKey and accessToken only in protected fields. Replace the accessToken when refreshed or expired. Leave the base URL, timeout, and response limit blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/device-list
```

In Dex Web **Start Flow**, choose `AqaraDevices` and a unique Flow ID. The input reads the first page with Aqara's default page size:

```json
{"pageNum":1,"pageSize":50}
```

The output contains `totalCount`, page information, and an array of devices with identity, model, and online state. The mock integration test exercises this Flow through a real Dex Worker and Client with synthetic data and no Aqara credentials.
