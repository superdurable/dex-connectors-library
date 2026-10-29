# Mixpanel connector

Mixpanel event import and bounded raw export for Dex. The connector uses the public REST APIs with a static service-account username and secret through HTTP Basic authentication.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `trackEvent` | `POST /import?strict=1&project_id=...` | `imported` after one valid event is accepted |
| `exportEvents` | `GET /api/2.0/export` with date and limit parameters | `exported` with a bounded JSONL batch |

`trackEvent` fixes event name, `distinct_id`, and Unix-second `time` in the operation input. It derives a UUID-shaped, 36-byte `$insert_id` from Dex's stable call ID. All four deduplication fields therefore remain identical on retries. Mixpanel applies query-time deduplication to that tuple. Raw exports can still contain duplicate ingested rows; the export operation intentionally returns raw events and does not claim deduplication. It strips arbitrary event properties from Flow output, retaining only event name, distinct ID, time, and insert ID.

## Authorization

Sign in at [Mixpanel Organization Settings](https://mixpanel.com/settings/org/), open **Service Accounts**, and choose **Create Service Account** with Owner or Admin access to the project. Copy the generated username into the secret `service_account_username` field and the secret shown once into `service_account_secret`. Open [Project Settings > Overview](https://mixpanel.com/settings/project/) and enter its positive numeric Project ID in `projectId`; blank cannot authenticate. Both service-account fields are sent only in the Basic authorization header; the non-secret project ID is a query parameter. Leave `baseUrl`, `exportBaseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed US defaults. For EU or India residency, set both host fields to the provider's matching regional endpoints.

## Failure behavior

| Condition | Route |
| --- | --- |
| Strict import confirms one event or export returns valid JSONL | `imported` or `exported` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 409, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [event track example](examples/event-track). Optional branches fail its Flow. There is no `uncertain` branch because Mixpanel deduplicates the stable event tuple in queries.

## Mocks

The generated `mixpanelmock` defaults accept a Meridian signup event and return a raw export row for that event. The example's mock-backed real-Dex test checks the imported event's distinct ID.

## Sources and verification

- [Import Events](https://docs.mixpanel.com/reference/import-events) documents POST, Basic auth, project ID, strict validation, JSON request and response, required event fields, and `$insert_id` constraints.
- [Download Data](https://docs.mixpanel.com/reference/raw-event-export) documents GET export, date and limit inputs, and JSONL output.
- [Export authentication](https://docs.mixpanel.com/reference/raw-data-export-api-authentication) documents service-account authentication with `project_id`.
- [Event Deduplication](https://docs.mixpanel.com/reference/event-deduplication) documents the four-field deduplication tuple and raw-export duplicate behavior.
- [Service Accounts](https://docs.mixpanel.com/reference/service-accounts) documents creation and Basic authentication.

Live Mixpanel service-account authorization, import, and export remain unverified. CI runs the mock-backed real-Dex test. The coordinator approved static service-account Basic credentials for this connector.
