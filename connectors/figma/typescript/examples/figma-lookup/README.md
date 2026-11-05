# Figma TypeScript lookup example

This Flow reads a file and its comments with the static Dex Web connection
name `figma`. `main.ts` starts the real Worker from the project environment;
`integration.ts` runs the same Flow against generated mock operations through
a real Dex Worker, Client, persistence, retry, and terminal branch.

From `connectors/figma/typescript`, install and build the package:

```bash
npm ci
npm run build
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-figma
```

Generate a strict Flow Definition Graph 2.0 from the checked-in source:

```bash
mkdir -p /tmp/figma-typescript-fdg
dexcli visualize ./examples/figma-lookup/flow.ts --language typescript \
  --schema-version 2.0 --json --out /tmp/figma-typescript-fdg
```

Use Dex CLI 1.33.0 or later; the local package link makes its generated
`dex-connector.json` available to the source analyzer. The command must
report `valid: true`. Start `dexcli dev` with the FDG
directory, configure the `figma` connection in Dex Web, and run
`npm run example:worker` with the project environment from the
[TypeScript SDK configuration guide](../../../../../sdkts/README.md#project-configuration).
Start `FigmaTypeScriptLookup` through Dex
Web using an input such as `{ "fileKey": "fileA" }`, replacing the sample key
with the key from a Figma file URL.

For a credential-free integration check, start `dexcli dev`, set
`DEX_FLOW_SERVICE_ADDRESS` to its Dex Server address, and run
`npm run example:integration`. The check uses generated mock answers and a
real Dex Worker and Client. Live Figma authorization and responses remain
unverified.
