# KuaidiBird connector

`QueryTracking` reads one carrier waybill's status and tracking events through KuaidiBird's [Instant Query API](https://www.kdniao.com/api-track), request type `1002`. The provider documents a limit of 500 requests per day. Its response is a current snapshot: the connector preserves event order exactly as returned. Some carriers can include contact details inside event descriptions, so applications should control access to the Flow result and Attribute.

In Dex Web **Connections**, create a `kuaidibird` connection and enter the decimal `business_id` (EBusinessID) and paired secret `api_key` (AppKey) from the [user management console](https://www.kdniao.com). The connection guide describes registration, verification, product enablement, and rotation. The input fields are `carrierCode` (uppercase code from the provider's carrier table) and `trackingNumber` (6–30 characters). Both are required. The connector does not place the API key or merchant ID in the Flow result, receipt, or logs.

A failed query returns the optional `providerRejected` branch; HTTP rate limits and server failures retry at most twice under the quota-aware policy. The runnable [parcel tracking Flow](go/examples/parcel-tracking/README.md) uses a static `kuaidibird` connection and synthetic mock data for real Dex integration testing. A live customer account is required to verify provider authorization and carrier responses.

For a complete Go application example, see the runnable Flow's `NewProjectConnection` wiring and `NewQueryTrackingStep` usage.
