# Box file download Flow

This runnable Flow calls `downloadFile` with the static `box` connection. Its successful result contains one file's stable identity, bounded Base64 content, and decoded byte count. The result is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/box/go`, run:

```bash
dexcli visualize ./examples/file-download/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/box-file-download-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Configure the `box / box` OAuth connection as described in the [connector README](../../../README.md), including Box's `root_readwrite` application scope. With `DEX_PROJECT_*` configured, run from `connectors/box/go`:

```bash
GOWORK=off go run ./examples/file-download
```

Start `BoxFileDownload` with a unique Flow ID and input `{"fileId":"12345"}`. Find the numeric file ID in the Box Web URL after `/files/`; it is not secret. Increase `maxDownloadBytes` in Dex Web only when the Flow should persist larger file content. The Worker has no inbound listener.

With `dexcli dev` serving at `127.0.0.1:8975`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8975 GOWORK=off go test -race -tags=integration ./examples/file-download/flow -count=1` for a mock-backed real Dex check. Live provider authorization remains unverified.
