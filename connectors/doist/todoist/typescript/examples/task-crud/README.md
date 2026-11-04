# Todoist task CRUD Flow

`TaskLifecycleFlow` uses the static `todoist` connection. It creates a disposable task, reads it, replaces its title and optional description, lists its project, and deletes it. The Flow stores only task data and nonsecret input; the personal API token remains in Dex Web Connections.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create a `todoist` connection with the personal API token from [Todoist Developer settings](https://app.todoist.com/app/settings/integrations/developer). Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file. From `connectors/doist/todoist/typescript`, start the Worker:

```sh
npm run example:task-crud
```

In another terminal with the same environment, start the disposable task lifecycle:

```sh
npm run example:task-crud:start -- examples/task-crud/input.example.json
```

The starter prints the final task projection and deletion result. Edit the input file to choose task text; never put the token there. The local integration test uses a Todoist stand-in and a real Dex Worker and Client:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:31320 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From the package directory:

```sh
npm run build
npm install --prefix examples/task-crud --no-save --no-package-lock --ignore-scripts .
cd examples/task-crud
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Todoist authorization and provider behavior have not been verified.
