# TextIn account balance Flow

This runnable Flow uses the static `textin` connection to read available T-coins and payment type. The `fetched` branch stores the two-field result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/textin/go`, run:

```bash
dexcli visualize ./examples/account-balance/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/textin-account-balance
```

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override textin=<artifact-directory>`. In Dex Web **Connections**, create the `textin` connection. Open [TextIn](https://www.textin.com/) > Workbench > Account Settings > Developer Information, then copy the App ID into `app_id` and secret code into `secret_code`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
GOWORK=off go run ./examples/account-balance
```

In Dex Web **Start Flow**, choose `TextInAccountBalance`, enter a unique Flow ID, and use empty input `{}`. The output includes `availableTCoins` as a decimal string and `payType` as 1 or 2. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential. Run it with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10011 GOWORK=off go test -tags integration ./examples/account-balance/flow` while `dexcli dev -dex-port 10011 -web-port 10012 -open=false` is running.
