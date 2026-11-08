# Dropbox file download Flow

This runnable Flow calls `downloadFile` with the static `dropbox` connection. Its successful result contains one file's stable identity, bounded Base64 content, and decoded byte count. The result is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/dropbox/go`, run:

```bash
dexcli visualize ./examples/file-download/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-file-download-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Configure the `dropbox / dropbox` OAuth connection as described in the [connector README](../../../README.md), including `files.content.read`. With `DEX_PROJECT_*` configured, run from `connectors/dropbox/go`:

```bash
GOWORK=off go run ./examples/file-download
```

Start `DropboxFileDownload` with a unique Flow ID and input `{"path":"/example.txt"}`. The path can also be a Dropbox `id:` or `rev:` identifier. Increase `maxDownloadBytes` in Dex Web only when the Flow should persist larger file content. The Worker has no inbound listener.

With `dexcli dev` serving at `127.0.0.1:8975`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8975 GOWORK=off go test -race -tags=integration ./examples/file-download/flow -count=1` for a mock-backed real Dex check. Live provider authorization remains unverified.
