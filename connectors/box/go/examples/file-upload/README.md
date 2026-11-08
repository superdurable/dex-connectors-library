# Box file upload Flow

This runnable Flow calls `uploadFile` with the static `box` connection. It replaces an existing file's current content with bounded Base64-decoded bytes, then saves only its ID, name, and SHA-1 in an Attribute and shows them through read-only Dex Web RPCs. When the current content already matches, it completes without creating another version. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/box/go`, run:

```bash
dexcli visualize ./examples/file-upload/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/box-file-upload-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Configure the `box / box` OAuth connection as described in the [connector README](../../../README.md), including Box's `root_readwrite` application scope. With `DEX_PROJECT_*` configured, run from `connectors/box/go`:

```bash
GOWORK=off go run ./examples/file-upload
```

Start `BoxFileUpload` with a unique Flow ID and input `{"fileId":"12345","contentBase64":"SGVsbG8="}`. Find the numeric file ID in the Box Web URL after `/files/`; it is not secret. The existing file name is preserved. Increase `maxUploadBytes` in Dex Web only when the Flow should carry larger content, up to Box's 50 MiB direct-upload limit. The Worker has no inbound listener.

With `dexcli dev` serving at `127.0.0.1:8975`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8975 GOWORK=off go test -race -tags=integration ./examples/file-upload/flow -count=1` for a mock-backed real Dex check. Live provider authorization remains unverified.
