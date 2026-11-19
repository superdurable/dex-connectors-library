# Zilliz TypeScript connector

`@superdurable/connector-zilliz` provides the read-only `listClusters` Dex Step. It reads the first page of at most 25 clusters visible to an API Key and returns cluster ID, name, region, plan, and status. The [Zilliz API Key guide](https://docs.zilliz.com/docs/manage-api-keys) describes read-only access.

Use the [runnable Flow and Worker](examples/cluster-list/) with the static `zilliz` connection. In Dex Web Connections, sign in at [Zilliz Cloud](https://cloud.zilliz.com), open your organization > API Keys > + API Key, create a Key with project Read-Only access, and save it in the connection. The Key is private and resolved per call; blank prevents a request. The endpoint and first-page limit are fixed.

Go-recorded provider scenarios cover successful, rejected, malformed, retried, and interrupted requests. The integration test uses a real Dex Worker and Client with a local 503-then-success provider. Live Zilliz authorization and project visibility remain unverified without a user-controlled Key.
