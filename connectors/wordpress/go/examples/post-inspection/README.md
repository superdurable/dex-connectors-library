# WordPress post inspection Flow

This runnable Flow calls `getPost` with the static `wordpress` connection and completes with a published post. Optional branches are unwired, so a missing post or provider failure fails the Flow.

## Strict FDG 2.0 generation

From `connectors/wordpress/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/post-inspection/flow/workflow.go --schema-version 2.0 --json --out ./build/post-inspection
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override wordpress=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `wordpress / wordpress` and follow the application-password guide at https://developer.wordpress.org/rest-api/using-the-rest-api/authentication/. Set `siteUrl` to the HTTPS Site Address from your WordPress `/wp-admin/options-general.php`, and create an application password under `/wp-admin/profile.php`. Leave timeout and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/wordpress/go`:

```bash
go run ./examples/post-inspection
```

In Dex Web Start Flow, choose `WordPressPostInspection`, enter a unique Flow ID, and use:

```json
{"postId":42}
```

Replace `42` with a published post's numeric ID. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show the persisted post. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/post-inspection/flow` while `dexcli dev` runs.
