# Harvest lookup Flow

This Flow uses the static `harvest` Dex Web connection. Its first Step lists projects and its second Step lists time entries. Both provider calls run inside connector Step Execute methods.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Configure the `harvest` connection with the secret `api_key` and nonsecret decimal `accountId` as described in the [package README](../../README.md). Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/harvest/typescript`, run:

```sh
npm run example:worker
```

In another terminal with the same environment, start the Flow:

```sh
npm run example:start -- examples/harvest-lookup/input.example.json
```

The starter prints the time entry page. No token belongs in the input file.

For a credential-free integration check against a real Dex stack, run `npm run integration:harvest-lookup` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex gRPC address. The generated mock supplies provider outcomes.

Generate and lint the strict Flow Definition Graph 2.0:

```sh
dexcli visualize examples/harvest-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/harvest-lookup-fdg --open=false
```
