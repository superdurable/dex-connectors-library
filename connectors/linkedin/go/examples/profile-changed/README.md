# LinkedIn profile-changed example

This example starts one `LinkedInProfileChange` Flow whenever the connected
LinkedIn member's own profile claims change, through the `profileChanged`
Trigger. It never watches any other member.

1. `RecordLinkedInProfileChange` validates the delivered change (previous and
   current profile plus the changed claims) and records it.
2. `ReadLinkedInVerifiedProfile` calls `linkedin.NewGetAuthenticatedProfileStep`,
   so the record holds the member's current verified profile.
3. `CompleteLinkedInProfileChange` records that profile and completes the Flow.
   The `verifiedEmailRequired` branch is also wired here: a change that
   unverified or removed the member's email is recorded without a verified
   profile instead of failing the Flow.

The Flow ID is `linkedin-profile-change-<Trigger event ID>`, and the Trigger
event ID is stable per change. A redelivered change therefore finds its
existing Flow instead of starting a second one. A rejected or invalid read
fails the Flow.

The first start records the member's current profile as a baseline and starts
no Flow for it. See the
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
- The `own-profile-changes` Trigger binding has no field: it always watches
  the connected member's own profile.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/linkedin/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/profile-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/profile-changed
```

Run `dexcli dev` with that build directory, open Dex Web at
`http://127.0.0.1:8802` rather than `localhost`, then run the Worker. It reads
the `DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/profile-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the stored profile commits, so a restarted Worker neither misses
nor repeats a change. Under `dexcli dev`, the poll cursor stays in process
memory, so a restarted local Worker records a new baseline. Changes made while
it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/profile-changed/...
GOWORK=off go test -tags=integration ./examples/profile-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for LinkedIn's UserInfo endpoint. They check that the baseline starts
no Flow, that each profile change starts exactly one Flow that reads the
verified profile back, that a change made while the Trigger process is stopped
arrives after a restart, and that a connection reauthorized as another member
records a new baseline without delivering the gap. Set
`DEX_FLOW_SERVICE_ADDRESS` when the server is not at `127.0.0.1:8801`. Live
LinkedIn polling has not been verified.
