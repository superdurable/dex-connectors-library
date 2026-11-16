# LinkedIn profile-changed example for Python

This example starts one `LinkedInProfileChangePython` Flow whenever the
connected member's own LinkedIn profile claims change, through the
`profileChanged` Trigger's `own-profile-changes` binding. It only watches the
member who authorized the `linkedin-profile-watcher` connection, never other
members.

1. `RecordLinkedInProfileChange` validates the delivered change and records
   it.
2. `ReadLinkedInVerifiedProfile` calls `new_get_authenticated_profile_step`, so
   the record holds the member's current verified profile.
3. `CompleteLinkedInProfileChange` records the profile read back and
   completes. The `verifiedEmailRequired` branch is also wired: a change that
   unverified or removed the email is recorded without a verified profile
   instead of failing the Flow.

The Flow ID is `linkedin-profile-change-<Trigger event ID>`, and the event ID
is stable per change, so a redelivered change finds its existing Flow. The
first start records the member's current profile as a baseline and starts no
Flow. See the
[connector README](../../../README.md#profilechanged-trigger) for the tracked
claims, picture re-signing, and rate limits.

## Configure

Follow the [LinkedIn setup](../../../README.md), then authorize the
`linkedin-profile-watcher` connection under **Connections** in Dex Web. The
Trigger reads `GET https://api.linkedin.com/v2/userinfo` with the `openid`,
`profile`, and `email` scopes the connection already requests; it needs no
new scope.

- **Poll interval** on the connection is the delay between polls, at least one
  minute. Each poll is one UserInfo request, counted against LinkedIn's daily
  per-member and per-app limits, and members rarely edit their profile, so
  prefer a long interval.
- The `own-profile-changes` binding has no field: it always watches the
  connected member's own profile.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/linkedin/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/profile_changed/flow.py --schema-version 2.0 --json --out ./build/profile_changed
dexcli dev -flow-rendering-dir ./build
```

Open Dex Web at `http://127.0.0.1:8802`, configure the connection, set
`DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote, and
run the Worker, which also runs the Trigger:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.profile_changed.worker
```

Set `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed. Trigger records, without
profile claims or credentials, go to the `dex_connectors.sdk.trigger` logger.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the stored profile commits, so a restarted Worker neither misses
nor repeats a change. Under `dexcli dev`, the poll cursor stays in process
memory, as in Go, so a restarted local Worker records a new baseline and a
change made while it was stopped is not delivered.

## Test

```bash
uv run pytest tests/test_profile_changed_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/profile_changed/integration_test.py
```

The integration tests run a real Dex Worker and Client against a fake of
LinkedIn's UserInfo endpoint, with project storage shared by two Trigger
processes. They check that the baseline starts no Flow, that each change
starts exactly one Flow, that a change made while the Trigger process is
stopped arrives after a restart, and that a connection re-authorized as
another member records a new baseline. Live LinkedIn polling has not been
verified.
