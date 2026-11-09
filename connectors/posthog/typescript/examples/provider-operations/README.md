# PostHog TypeScript examples

The Worker registers `PostHogTypeScriptEventCapture` and
`PostHogTypeScriptInsightList` using the static `posthog` connection. Configure
the project token and, for insight listing, a scoped personal API key in Dex
Web. Run `npm run example:worker` with `DEX_PROJECT_*` and
`DEX_FLOW_SERVICE_ADDRESS` set.

Event capture input:

```json
{"event":"Signed Up","distinctId":"member-1","timestamp":"2026-10-29T00:00:00Z"}
```

Saved insight listing input:

```json
{"projectId":42,"offset":0,"limit":20}
```

Generate strict FDG 2.0 with
`dexcli visualize examples/provider-operations/event-capture-flow.ts --schema-version 2.0 --json --out /tmp/posthog-typescript-event-fdg --lint app`.
Run the same command for `insight-list-flow.ts` to generate its FDG.
The integration test runs both Flows through a real Dex Worker and Client with
generated mocks. Live PostHog authorization and ingestion remain unverified.
