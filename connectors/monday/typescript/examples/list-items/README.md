# List monday.com items

`ListItemsFlow` reads one bounded page from a board through the static
`monday-board` connection. The Flow persists only the nonsecret request and
returned page. Its `listed` branch completes the Flow; an unwired optional
branch fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the `monday-board` Dex Web connection with either a personal API
token or the monday.com OAuth 2.1 authorization described in the connector
manifest. Point `DEX_CONNECTOR_CONFIG_FILE` at the private, mode-0600
`connections.json` produced by Dex Web. From `connectors/monday/typescript`:

```sh
npm ci
npm run example:list
```

In another terminal with the same configuration, set the board ID in
`examples/list-items/input.example.json` and run:

```sh
npm run example:list:start -- examples/list-items/input.example.json
```

The board ID is the decimal number after `/boards/` in the board URL; it is
nonsecret. The token and OAuth material stay in Dex Web Connections.

To check Worker, Client, persistence, retry, and branch routing without
provider credentials, start `dexcli dev` and run `npm run integration:list`.
The test uses the same Flow with generated mock operations on a real Dex stack.

Generate strict Flow Definition Graph 2.0 source from `flow.ts` using
`dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false`.
Dex CLI 1.33.0 currently reports `dynamic_class` and `dynamic_step_target` for
the operation factory's completion target, followed by `unknown_step_target`
and `unreachable_step`. The real Worker and Client integration test above
verifies the registered Flow while strict analyzer support is pending.
