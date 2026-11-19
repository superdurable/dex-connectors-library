# EMQX Cloud TypeScript connector

`@superdurable/connector-emq` provides `listDeployments`, a read-only Query for EMQX Cloud deployments in the Platform API key's project scope. It returns bounded deployment metadata and omits private provider fields.

In [EMQX Cloud Console](https://cloud.emqx.com/console/), open **Settings > Platform API Key**, create a key with Deployment Management and the required project scopes, then save its key and secret as Dex secret credentials. Copy the Platform API endpoint ending in `/public_api/v1` to the connection's `platformEndpoint`. Blank credentials or endpoint prevent requests; leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Flow](examples/basic/) uses static connection name `emq`. `npm run check` replays the Go-recorded provider scenarios, including errors and retries. Live authorization and deployment data remain unverified without a user-controlled connection.
