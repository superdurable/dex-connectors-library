# Qichacha enterprise basic details Flow

This runnable Flow uses the static `qichacha` connection to look up one mainland enterprise. The `fetched` branch stores the three-field result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/qichacha/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/enterprise-basic-details/flow/workflow.go --schema-version 2.0 --json --out ./build/enterprise-basic-details
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override qichacha=<artifact-directory>`. In Dex Web **Connections**, create the `qichacha` connection. At [Qichacha API 410](https://openapi.qcc.com/dataApi/410), sign in, complete enterprise verification and application-scenario approval, then get the paired AppKey and SecretKey from **账号安全**. Save both in the secret fields. Leave the endpoint, timeout, and response size blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/enterprise-basic-details
```

In Dex Web **Start Flow**, choose `QichachaEnterpriseBasicDetails` and enter a unique Flow ID:

```json
{"keyword":"示例科技有限公司"}
```

The result contains `name`, `creditCode`, and `registrationStatus`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
