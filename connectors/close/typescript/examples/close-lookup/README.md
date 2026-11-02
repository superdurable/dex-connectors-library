# Close lead lookup

`CloseLookupFlow` lists ten leads from a selected zero-based offset, then reads
the first lead by ID. The static Dex Web connection name is `close`.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the private Dex Web `close` connection using a token created at
[Settings > Developer > API Keys](https://app.close.com/settings/api/).
Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web
`connections.json` file. From `connectors/close/typescript`, start the Worker:

```sh
npm run example:lookup
```

In another terminal with the same environment, start the Flow:

```sh
npm run example:lookup:start -- examples/close-lookup/input.example.json
```

The starter prints the safe lead result. No API key belongs in the input file.
The real Dex integration test uses a local Close HTTP fixture and covers both
provider reads plus a failed Flow from an unwired provider rejection:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21240 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or
newer. From `connectors/close/typescript`:

```sh
npm run build
npm install --prefix examples/close-lookup --no-save --no-package-lock --ignore-scripts .
cd examples/close-lookup
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Close provider access has not been verified.
