# Mixpanel TypeScript connector

`@superdurable/connector-mixpanel` implements `trackEvent` and `exportEvents` for Dex. Version `0.1.1` implements both Go operation identities and no Triggers.

Create a `MixpanelClient` with a positive numeric `projectId` and a credential provider that returns `serviceAccountUsername` and `serviceAccountSecret` as `SecretString` values. The credentials are resolved for each attempt and sent only as an HTTP Basic header. The default ingestion and export hosts are the US Mixpanel endpoints; configure both URLs for a regional project. `timeoutMs` defaults to 15,000 and `maxResponseBytes` to 2,097,152. Dex project connections accept the manifest's `timeout` as a Go duration such as `1m` or an integer nanosecond value.

`trackEvent` requires a stable event name, distinct ID, and Unix-second timestamp. It derives `$insert_id` from the Dex Call ID so retries preserve Mixpanel's deduplication tuple, and it is declared `repeatOnUnknown: safe`: only HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422 selects `providerRejected`, and every other unknown answer retries. `exportEvents` accepts an inclusive date range no longer than 30 days and a limit from 1 through 100, and returns only event name, distinct ID, time, and insert ID. The generated `newTrackEventStep` and `newExportEventsStep` route their happy branches; optional branches fail a Flow when unwired.

The [runnable event-track example](examples/event-track/) uses the static `mixpanel` connection. The [root connector README](../README.md) explains authorization and provider behavior. Tests replay [Go-recorded conformance scenarios](../../../conformance/connectors/mixpanel/scenarios.json). Live provider authorization, import, and export remain unverified.
