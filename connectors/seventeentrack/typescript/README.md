# 17TRACK TypeScript connector

`@superdurable/connector-seventeentrack` implements the read-only `listTrackedShipments` Step. It calls `POST /track/v2.4/gettracklist` to list registered shipment numbers and tracking statuses without contacts or addresses.

Use the [authorization guide](../README.md#authorization) to configure the static `seventeentrack` connection in Dex Web. The [runnable Flow and Worker](examples/shipment-list/) use that connection. The API key stays outside Flow state and is sent only in the `17token` header.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real 17TRACK account remains unverified.
