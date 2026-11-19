# Enlvon TypeScript connector

`@superdurable/connector-enlvon` provides the read-only `searchPolicies` Query for electricity policy cards. Its output contains source title, date, source identifier, and original government URL. It omits full text, analysis, relevance scores, and credential values.

Read the [Enlvon API guide](https://enlvon.com/docs), select **申请内测**, and request a dedicated programmatic API Key. Store the `enl_` prefixed key as a Dex secret credential. A blank key prevents requests. Leave baseUrl, timeout, and maxResponseBytes blank for their displayed production defaults. Query input requires a nonempty natural-language string and optional `topK` from 1 to 50; zero uses the provider default.

The [runnable Flow](examples/basic/) uses static connection name `enlvon`. `npm run check` replays all Go-recorded provider scenarios, including malformed responses, retries, and secret-safe output. Live provider authorization remains unverified without a user-controlled connection.
