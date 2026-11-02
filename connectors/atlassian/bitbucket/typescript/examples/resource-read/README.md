# Read Bitbucket repositories

`ResourceReadFlow` reads one repository page using the static `bitbucket-resource-read` connection. The provider call runs inside the generated Query Step. The Flow persists only the safe page.

Start `dexcli dev` on port 21270 and set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21270`. In Dex Web Connections, configure `bitbucket-resource-read` with a Bitbucket workspace access token granting Repositories > Read. Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file. From this package directory, run the Worker:

```sh
npm run example:list
```

In another terminal with the same configuration, edit `input.example.json` to name your workspace slug and start the Flow:

```sh
npm run example:list:start -- examples/resource-read/input.example.json
```

No credential belongs in the input file. The starter prints the repository page.

For a real Dex Worker/Client test against a local provider fixture, run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21270 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From the package directory:

```sh
npm run build
npm install --prefix examples/resource-read --no-save --no-package-lock --ignore-scripts .
cd examples/resource-read
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Bitbucket authorization and provider response behavior remain unverified.
