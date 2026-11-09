# PostHog Python connector

The Python package implements `captureEvent` and `listInsights` with the same
request and outcome decisions as the Go connector. Configure the PostHog project
token for event capture and a personal API key with `insight:read` for listing.

The runnable examples in `examples/event_capture` and `examples/insight_list`
use the static `posthog` connection. Event capture fixes its timestamp so
retries keep the same provider UUID. Both examples have real Dex mock integration
tests; provider authorization and live ingestion remain unverified.
