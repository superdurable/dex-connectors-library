# PostHog insight-list Flow

This runnable Flow calls `listInsights` with the static `posthog` connection and a project ID. Its happy-path page of saved insight IDs and names is stored in an Attribute and shown through read-only Dex Web RPCs. Optional provider branches are unwired and fail the Flow. Query results and the personal API key never enter Flow state.

## Strict FDG 2.0 generation

From `connectors/posthog/go`, run:

```bash
dexcli visualize ./examples/insight-list/flow/workflow.go --schema-version 2.0 --json --out /tmp/posthog-insight-list-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build a connector release artifact and pass it to `dexcli dev` with `--connector-release-override posthog=<artifact-directory>`. In Dex Web Connections choose `posthog / posthog`. Copy the Project API key from PostHog **Settings > Project > Project variables** and create a personal API key under **avatar > Account settings > Personal API Keys** with `insight:read` scope. Save both in Dex secret fields. For EU Cloud set `appUrl` to `https://eu.posthog.com` and `captureUrl` to `https://eu.i.posthog.com`; US Cloud uses the displayed defaults.

With `DEX_PROJECT_*` configured, run from `connectors/posthog/go`:

```bash
GOWORK=off go run ./examples/insight-list
```

Start `PostHogInsightList` with a unique Flow ID and input `{"projectId":42,"offset":0,"limit":20}`, replacing the project ID with the numeric ID in your PostHog project URL or Settings. The pull Worker connects to Dex Server without an inbound listener.

`GOWORK=off go test -race ./...` checks provider and Flow behavior. The integration test runs against a real Dex Worker with the generated provider mock.
