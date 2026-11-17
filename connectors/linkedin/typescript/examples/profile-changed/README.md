# LinkedIn profile-changed example (TypeScript)

`ProfileChangeFlow` records each change of the connected member's own UserInfo
profile. It declares `linkedin-profile-watcher` and the fieldless
`own-profile-changes` Trigger binding. Each event starts the Flow
`linkedin-profile-change-<event ID>`, so redelivery finds the existing Flow.
The first poll stores a baseline and starts no Flow.

Create or select a LinkedIn app at <https://www.linkedin.com/developers/apps>.
Under **Products**, enable **Sign in with LinkedIn using OpenID Connect**.
Under **Auth**, register the exact Dex Web redirect URI and copy the Client ID
and Client Secret to the Dex Web **Connections** form. Consent to `openid`,
`profile`, and `email`. Dex stores the access and optional programmatic refresh
tokens in the connection; do not put them in Flow input.

The connection's **Poll interval** controls the time between UserInfo requests,
with a minimum of one minute. Each poll counts against the provider's API
limits, so choose a longer interval when prompt detection is unnecessary. The
Trigger binding has no field: it always watches the connected member. Restart
the Worker after saving connection settings.

From `connectors/linkedin/typescript`, build the package and generate strict
FDG 2.0:

```bash
npm ci && npm run build
npm install --prefix examples/profile-changed --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/profile-changed/flow.ts --language typescript --schema-version 2.0 --lint app --json --out build/profile-changed --open=false
```

Start `dexcli dev -flow-rendering-dir ./build`, configure the connection in
Dex Web, then set `DEX_CONNECTOR_CONFIG_FILE` to its `connections.json` file and
run the Worker:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json npm run example:profile-changed
```

Hosted project storage records a change in the Trigger inbox before the poll
cursor commits. The integration test uses one shared project store across two
Worker processes and verifies delivery after restart and no duplicate on an
unchanged poll. `dexcli dev` uses a process-local poll cursor, so restarting
that local Worker seeds a new baseline.

```bash
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

The tests use a local UserInfo stand-in. Live LinkedIn authorization and API
polling have not been verified.
