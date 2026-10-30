# Calendly scheduling-link example

This Worker runs `CalendlyCreateSchedulingLink`. Start it with a Calendly event
type URI. The `CreateSchedulingLink` Step makes one fenced POST, and the
`CompleteSchedulingLink` Step records the accepted booking URL in the
`calendly-scheduling-link` Attribute before completing the Flow. Only the
`created` branch is wired; an unwired optional branch fails the Flow.

## 1. Generate the Flow Definition

From `connectors/calendly`, use the latest stable `dexcli` to generate strict
FDG 2.0. The repository's `script/dex_compatibility.py install-dexcli` command
can install that CLI in a chosen output path.

```bash
mkdir -p build
dexcli visualize ./examples/scheduling-link/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/scheduling-link
```

The result must say `valid: true`. The `created` branch leads to
`CompleteSchedulingLink`, and the summary and display views show the saved
link. A local unreleased connector may warn `connector_release_required`.

## 2. Start Dex and configure Calendly

Follow [invitee-recorder steps 2 and 3](../invitee-recorder/README.md#2-start-dex-with-this-connectors-release-metadata)
to build the connector release override, start `dexcli dev` with
`--flow-rendering-dir "$PWD/connectors/calendly/go/build"`, and save the
`calendly-scheduling` connection. The Worker uses the same static connection
name. A personal access token needs `scheduling_links:write`. The connector's
authorization guide in Dex Web gives the provider page and setup steps.

## 3. Run the Worker

From `connectors/calendly` in another terminal:

```bash
go run ./examples/scheduling-link
```

The Worker reads the `DEX_PROJECT_*` configuration described in
[project configuration](../../../../../sdkgo/projectconfig/README.md) and uses
`DEX_FLOW_SERVICE_ADDRESS` when supplied. Start the Flow in Dex Web with a
`CreateSchedulingLinkInput` such as:

```json
{"eventTypeUri":"https://api.calendly.com/event_types/TYPE0001"}
```

Choose an event type URI in the connected Calendly account. A successful run
returns one `SchedulingLink` with `bookingUrl` and `eventTypeUri`. A 503 before
acceptance is retried through a durable checkpoint; an accepted link is never
POSTed again when a later attempt cannot confirm its outcome.

## Test

From `connectors/calendly`, run `GOWORK=off go test -race -count=1 ./...` for
deterministic Flow and connector checks. The `integration` tagged test starts
this Worker on a real Dex stack and proves 503 then 201 completes with exactly
two identical POST bodies; CI runs it.
