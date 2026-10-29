# Klaviyo connector

Klaviyo profile lookup and deduplicated event submission for Dex. The connector uses the public REST/JSON API at revision `2026-10-15` with a private API key.

| Operation | Endpoint | Happy branch |
| --- | --- | --- |
| `getProfile` | `GET /api/profiles/{id}` | `found` with profile ID and email |
| `createEvent` | `POST /api/events` | `accepted` after HTTP 202 |

`createEvent` derives `unique_id` as the SHA-256 hex digest of Dex's stable connector call ID and sends it in `data.attributes.unique_id`. Retries reuse the same value. Klaviyo deduplicates on `(profile, metric, unique_id)`, so a retry cannot produce a second event for the same logical call. The 202 response means the event was validated and queued, not that asynchronous processing completed. Klaviyo may silently drop events for test email domains such as `example.com` and `test.com`.

## Authorization

Sign in at [Klaviyo API keys](https://www.klaviyo.com/settings/account/api-keys), open **Settings > API keys**, and select **Create Private API Key**. Name the key, grant `profiles:read` and `events:write`, create it, and copy the `pk_`-prefixed secret once. Paste it into Dex Web Connections' secret `private_api_key` field. Blank prevents requests. The secret travels only in `Authorization: Klaviyo-API-Key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Override for a compatible gateway, a slower network, or larger profile responses.

## Failure behavior

| Condition | Route |
| --- | --- |
| Profile found or event accepted | `found` or `accepted` |
| Profile missing | `notFound` |
| Definite provider rejection | `providerRejected` |
| Malformed or oversized profile response | `invalidResponse` |
| Invalid input or missing credentials | `defect` |
| Transport failure, 408, 409, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [profile event example](examples/profile-event). Other branches fail its Flow. There is no `uncertain` branch because the unique ID makes event submission repeat-safe.

## Mocks

The generated `klaviyomock` defaults include a Meridian profile and an accepted signup event. The example's mock-backed real-Dex test checks that the event operation receives the profile email.

## Sources and verification

- [Get Profile](https://developers.klaviyo.com/en/reference/get_profile) documents the endpoint, `profiles:read`, revision header, and HTTP 200.
- [Create Event](https://developers.klaviyo.com/en/reference/create_event) documents the endpoint, `events:write`, revision header, and HTTP 202.
- [Events API overview](https://developers.klaviyo.com/en/reference/events_api_overview) documents `unique_id` deduplication, event JSON, asynchronous processing, and test-domain behavior.
- [Authenticate API requests](https://developers.klaviyo.com/en/docs/authenticate_) documents private key creation, scopes, and the authorization header.

Live Klaviyo authorization, event delivery, and profile responses remain unverified. CI runs the mock-backed real-Dex test.
