# Zhihuiya TypeScript connector

`@superdurable/connector-zhihuiya` provides the read-only `getPatentValuation` Dex Step. It reads one valuation by patent publication number and returns the provider's patent ID, US dollar estimate, and five numeric scores. The [provider's patent valuation API](https://connect.zhihuiya.com/patent-valuation-data/patent_valuation) defines this response.

Use the [runnable Flow and Worker](examples/patent-valuation/) with the static `zhihuiya` connection. In Dex Web Connections, create an API Key through the provider's developer console described in the [connector authorization guide](../connector.yaml), then save the private `sk-` Key. Blank prevents a request. Supply a publication number such as `JP6012277B2` when starting the Flow.

Go-recorded provider scenarios cover found, no-data, rejected, malformed, retried, and interrupted requests. The integration test uses a real Dex Worker and Client with a local 503-then-success provider. Live Zhihuiya authorization and valuation availability remain unverified without a user-controlled Key.
