# Get a BambooHR employee

`GetEmployeeFlow` reads requested fields with the static
`bamboohr-employees` connection and retains the safe employee record. The
provider call runs inside the connector Step. An unwired optional branch
fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure a private Dex Web Connection named `bamboohr-employees`: set
`companyDomain` and save the BambooHR API key as `api_key`, following the
[connector setup instructions](../../../README.md#bamboohr-setup). Set
`DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` path.
From `connectors/bamboohr/typescript`, start the Worker:

```sh
npm run example:get
```

In another terminal with the same environment, edit `input.example.json` for
an employee the key can view, then start the Flow:

```sh
npm run example:get:start -- examples/get-employee/input.example.json
```

The starter prints the returned employee fields. The API key stays in the
private connection and never enters the input file.

Run the real Dex integration test against a local BambooHR HTTP fixture:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21150 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0
or newer:

```sh
npm run build
npm install --prefix examples/get-employee --no-save --no-package-lock --ignore-scripts .
cd examples/get-employee
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The installed local CLI 1.24.0 has no TypeScript visualization support, so
strict FDG generation was not run here. Live BambooHR authorization and Dex
Web connection setup have not been exercised.
