# LinkedIn profile-changed example for TypeScript

This example starts one `LinkedInProfileChangeTypeScript` Flow whenever the
connected LinkedIn member's own profile claims change, through the
`profileChanged` Trigger's `own-profile-changes` binding. It never watches any
other member.

1. `RecordLinkedInProfileChange` validates the delivered change (previous and
   current profile plus the changed claims) and records it.
2. `ReadLinkedInVerifiedProfile` calls `newGetAuthenticatedProfileStep`, so the
   record holds the member's current verified profile.
3. `CompleteLinkedInProfileChange` records that profile and completes the Flow.
   The `verifiedEmailRequired` branch is also wired here: a change that
   unverified or removed the member's email is recorded without a verified
   profile instead of failing the Flow.

The Flow ID is `linkedin-profile-change-<Trigger event ID>`, and the event ID
is stable per change, so a redelivered change finds its existing Flow. The
first start records the member's current profile as a baseline and starts no
Flow for it. See the
[connector README](../../../README.md#profilechanged-trigger) for tracked
claims, picture URLs, resets, and rate limits.

## Configure

Create the LinkedIn app and authorize the `linkedin-profile-watcher`
connection under **Connections** in Dex Web, following the authorization guide
there. The Trigger and the Flow both use the `openid profile email` scopes the
connection already requests.

- **Poll interval** on the connection is the delay between polls, at least one
  minute. Each poll is one UserInfo call, and LinkedIn limits calls per member
  and per app per day, so keep it long; profiles change rarely.
- The `own-profile-changes` binding has no field: it always watches the
  connected member's own profile.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/linkedin/typescript`, build the package, make it resolvable
from the example, and generate strict FDG 2.0:

```bash
npm ci && npm run build
npm install --prefix examples/profile-changed --no-save --no-package-lock --ignore-scripts .
cd examples/profile-changed
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/profile-changed --open=false
```

Run `dexcli dev -flow-rendering-dir ./build`, configure the connection in Dex
Web, set `DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` Dex Web wrote,
and start the Worker, which also runs the Trigger:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:profile-changed
```

Set `LOG_LEVEL` to `debug`, `info`, `warn`, or `error`; Trigger records carry
no profile claims or credentials.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the stored profile commits, so a restarted Worker neither misses
nor repeats a change. Under `dexcli dev`, the poll cursor stays in process
memory, as in Go, so a restarted local Worker records a new baseline and
changes made while it was stopped are not delivered.

## Test

```bash
npx vitest run test/profile_changed_workflow.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration/profile-changed.integration.test.ts
```

The integration tests run `runProfileChangeWorker` on a real Dex Worker and
Client against a stand-in for LinkedIn's UserInfo endpoint, with project
storage shared by two processes. They check that the baseline starts no Flow,
that each profile change starts exactly one Flow that reads the verified
profile back, that a change made while the process is stopped arrives after a
restart, and that a connection reauthorized as another member records a new
baseline without delivering the gap. Live LinkedIn polling has not been
verified.
