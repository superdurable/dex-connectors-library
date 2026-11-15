# Confluence page-changed example

This example starts one `ConfluencePageChange` Flow for every page version
created or updated in the picked space, through the `pageChanged` Trigger:

1. `RecordPageChange` validates the delivered change and records it.
2. `ReadChangedPage` calls `confluence.NewGetPageStep`, so the record holds the
   page's current title and up to 2,000 characters of its body as Markdown.
3. `CompletePageChange` records the page read back and completes the Flow.

The Flow ID is `confluence-page-change-<Trigger event ID>`, and the Trigger
event ID is stable per page version. A redelivered version therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A page deleted before it is read, or a rejected or invalid
read, fails the Flow.

The first start records recently changed pages as a baseline and starts no
Flow for them. See the
[connector README](../../../README.md#pagechanged-trigger) for the query, the
ten-minute lookback, change kinds, pagination, and rate limits.

## Configure

Follow the [Confluence connector setup](../../../README.md#atlassian-setup),
then configure the `confluence-page-watcher` connection under **Connections**
in Dex Web. Leave `cloudId` blank when the authorization covers one Confluence
site.

- **pollInterval** on the connection is the delay between polls, at least 10
  seconds. Shorter intervals deliver changes sooner and spend more of the
  OAuth app's Confluence quota.
- **Watched space** on the `space-page-changes` Trigger binding lists the
  current spaces on the connection's site that the account can view, the same
  spaces listed at `https://<your-site>.atlassian.net/wiki/spaces`. It saves
  the space ID, key (such as `OPS`), and name, which are not secret and are
  stored in the project configuration. Leave it unsaved to watch every space
  the account can view.

The Worker reads the saved values once at startup, so restart it after saving.
A changed space records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/confluence/go` with the
latest stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/page-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/page-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/page-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the scan position commits, so a restarted Worker neither misses
nor repeats a version. Under `dexcli dev`, the poll cursor stays in process
memory, so a restarted local Worker records a new baseline. Changes made while
it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/page-changed/...
GOWORK=off go test -tags=integration ./examples/page-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against an in-process
Confluence site that serves accessible resources, CQL content search with
cursors, and page reads. They check that the baseline starts no Flow, that
pages in other spaces are ignored, that each version starts exactly one Flow,
and that versions saved while the Trigger process is stopped arrive after a
restart whose first search answers 429 with `Retry-After`. They also check
that a changed space records a new baseline. Live Confluence search has not
been verified.
