# Dropbox folder discovery Flow

This runnable Flow calls `listFolder` with the static `dropbox` connection. Its successful result is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Only entry IDs, names, display paths, kinds, and the continuation cursor enter the Flow result; OAuth tokens do not.

## Strict FDG 2.0 generation

From `connectors/dropbox/go`, run:

```bash
dexcli visualize ./examples/folder-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-folder-discovery-fdg --open=false
```

The graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override dropbox=<artifact-directory>`. In Dex Web Connections select `dropbox / dropbox`. Use the [Dropbox Developer Console](https://www.dropbox.com/developers/apps) to create a Scoped access app, choose App folder or Full Dropbox, enable `files.metadata.read`, register the exact Redirect URI shown by Dex Web, and copy App key and App secret into the authorization form. Complete Dropbox consent as the file owner. Blank timeout and response-size fields use their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/dropbox/go`:

```bash
GOWORK=off go run ./examples/folder-discovery
```

Start `DropboxFolderDiscovery` with a unique Flow ID and input `{"path":"/Projects","limit":20}`. Use an empty path for the app's root. The output cursor reports where a later continuation request can resume; this initial operation does not fetch subsequent pages. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8971`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8971 GOWORK=off go test -race -tags=integration ./examples/folder-discovery/flow -count=1`. Live provider authorization remains unverified.
