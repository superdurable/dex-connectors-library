# Read Microsoft Teams thread replies

Create a Dex Web OAuth connection named `microsoft-teams-incident` using the [connector guide](../../../README.md). From `connectors/microsoft/teams/typescript`, run `npm ci`, start `npm run example:thread-replies`, then run `npm run example:thread-replies:start -- examples/list-thread-replies/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex port. The Flow stores one bounded reply page; an unwired optional branch fails the Flow.

The integration test runs this Flow through a real Dex Worker and Client against a local Graph stand-in. Live Microsoft authorization and Graph calls remain unverified.

Generate strict Flow Definition Graph 2.0 with the latest stable Dex CLI:

```sh
npm run build
npm install --prefix examples/list-thread-replies --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/list-thread-replies/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
