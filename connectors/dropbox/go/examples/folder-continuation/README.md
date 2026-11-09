# Dropbox folder continuation Flow

This runnable Flow calls `continueFolder` with the static `dropbox` connection. Its input is a cursor returned by an earlier `listFolder` or `continueFolder` call. The typed page is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. An expired cursor selects `cursorExpired`, after which the caller must start a new folder listing.

## Strict FDG 2.0 generation

From `connectors/dropbox/go`, run:

```bash
dexcli visualize ./examples/folder-continuation/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-folder-continuation-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Configure the `dropbox / dropbox` OAuth connection as described in the [connector README](../../../README.md). With `DEX_PROJECT_*` configured, run from `connectors/dropbox/go`:

```bash
GOWORK=off go run ./examples/folder-continuation
```

Start `DropboxFolderContinuation` with a unique Flow ID and input `{"cursor":"<cursor from listFolder>"}`. The Worker has no inbound listener. With `dexcli dev` serving at `127.0.0.1:8973`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8973 GOWORK=off go test -race -tags=integration ./examples/folder-continuation/flow -count=1` for a mock-backed real Dex check. Live provider authorization remains unverified.
