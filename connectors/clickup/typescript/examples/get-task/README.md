# Read a ClickUp task

`GetTaskFlow` reads one task with the static `clickup-workspace` connection and returns its details. The input file contains only the nonsecret `taskId`.

Start `dexcli dev` with a gRPC port in 21100–21199. Configure the private Dex Web Connection `clickup-workspace` with a ClickUp personal API token from [Settings > Apps](https://app.clickup.com/settings/apps), then point `DEX_CONNECTOR_CONFIG_FILE` to the Dex Web `connections.json` file. Start the Worker from `connectors/clickup/typescript`:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21151 npm run example:task
```

In another terminal with the same project configuration, use a task ID from its `/t/` URL:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21151 npm run example:task:start -- examples/get-task/input.example.json
```

Run the real Dex integration test against the local ClickUp HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21151 npm run test:integration
```

Generate strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or later:

```sh
npm run build
npm install --prefix examples/get-task --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/get-task/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live ClickUp authorization and task reads have not been verified.
