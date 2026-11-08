# WordPress post creation Flow

This runnable Flow calls `createPost` with the static `wordpress` connection and completes with the new post's numeric ID and status. Empty status creates a draft for editorial review. Optional branches, including `uncertain`, are unwired so they fail the Flow. If creation becomes uncertain, inspect WordPress Posts before considering a new Flow.

## Strict FDG 2.0 generation

From `connectors/wordpress/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/post-creation/flow/workflow.go --schema-version 2.0 --json --out ./build/post-creation
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override wordpress=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `wordpress / wordpress`. Set the HTTPS Site Address from `/wp-admin/options-general.php`; create an application password under `/wp-admin/profile.php`. The account needs `edit_posts` for drafts and `publish_posts` for publishing. Leave timeout and size limits blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/wordpress/go`:

```bash
go run ./examples/post-creation
```

In Dex Web Start Flow, choose `WordPressPostCreation`, enter a unique Flow ID, and use:

```json
{"title":"Meridian release","content":"<p>Draft for review.</p>"}
```

To publish immediately, add `"status":"publish"`. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show only the created post ID and status. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/post-creation/flow` while `dexcli dev` runs.
