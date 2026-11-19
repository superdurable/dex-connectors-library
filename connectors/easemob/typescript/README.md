# Easemob TypeScript connector

`@superdurable/connector-easemob` provides the read-only `getUserDetail` Query for one registered IM user. It returns account ID, UUID, creation and modification timestamps, activation state, and optional nickname. Provider push tokens and unrelated private fields stay out of Flow results.

In [Easemob Console](https://console.easemob.com), open **Application List > select app > Application Overview** and copy the REST API host as `baseUrl`. Under **Development Configuration**, copy the `orgname#appname` App Key, Client ID, and secret Client Secret into Dex Connections. Blank values prevent requests. Leave timeout and maxResponseBytes blank for displayed defaults. Use a registered lowercase user ID as Query input.

The [runnable Flow](examples/basic/) uses static connection name `easemob`. `npm run check` replays 16 Go-recorded scenarios covering App Token acquisition, 401 refresh, Retry, malformed responses, and filtered output. Live provider authorization remains unverified without a user-controlled connection.
