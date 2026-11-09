# PostHog event-capture Flow

This runnable Flow captures one event with the static `posthog` connection. Its start Step fixes an omitted timestamp once, and `captureEvent` derives a stable UUID from Dex's call identity. The `accepted` branch completes after PostHog acknowledges processing; it does not prove later ingestion. Optional branches are unwired and fail the Flow. A repeated event may briefly appear in downstream live destinations before PostHog's eventual deduplication.

## Strict FDG 2.0 generation

From `connectors/posthog/go`, run:

```bash
dexcli visualize ./examples/event-capture/flow/workflow.go --schema-version 2.0 --json --out /tmp/posthog-event-capture-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata is supplied.

## Run with Dex Web

Build a connector release artifact and pass it to `dexcli dev` with `--connector-release-override posthog=<artifact-directory>`. In Dex Web Connections, choose `posthog / posthog`. Find the Project API key under PostHog Settings > Project > Project variables and save it in the Dex secret field. For EU Cloud set `captureUrl` to `https://eu.i.posthog.com` and `appUrl` to `https://eu.posthog.com`; US Cloud uses the displayed defaults. A personal API key is not needed for this Flow.

With `DEX_PROJECT_*` configured, run from `connectors/posthog/go`:

```bash
GOWORK=off go run ./examples/event-capture
```

Start `PostHogEventCapture` with a unique Flow ID and input `{"event":"Signed Up","distinctID":"member-1"}`. You may supply an RFC3339 `timestamp`; otherwise the start Step records the current instant once. The pull Worker connects to Dex Server without an inbound listener.

`GOWORK=off go test -race ./...` checks deterministic timestamp preparation and provider behavior. The integration test runs against a real Dex Worker with the generated provider mock.
