# Dropbox file upload Flow

This runnable Flow calls `uploadFile` with the static `dropbox` connection. It replaces one path with bounded Base64-decoded content, then saves only the resulting file identity in an Attribute and shows it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/dropbox/go`, run:

```bash
dexcli visualize ./examples/file-upload/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-file-upload-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Configure the `dropbox / dropbox` OAuth connection as described in the [connector README](../../../README.md), including `files.content.write`. With `DEX_PROJECT_*` configured, run from `connectors/dropbox/go`:

```bash
GOWORK=off go run ./examples/file-upload
```

Start `DropboxFileUpload` with a unique Flow ID and input `{"path":"/example.txt","contentBase64":"SGVsbG8="}`. The absolute path selects the replacement file. Increase `maxUploadBytes` in Dex Web only when the Flow should carry larger content. The Worker has no inbound listener.

With `dexcli dev` serving at `127.0.0.1:8975`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8975 GOWORK=off go test -race -tags=integration ./examples/file-upload/flow -count=1` for a mock-backed real Dex check. Live provider authorization remains unverified.
