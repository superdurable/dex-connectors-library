# Doc2X TypeScript connector

`@superdurable/connector-doc2x` provides `getParseStatus` for an existing Doc2X PDF parsing task. It reads `status` and `progress` (percent, 0–100), without returning PDF text, result URLs, or provider failure details.

In Dex Web Connections, sign in at [Doc2X Open Platform](https://open.noedgeai.com), open **API Keys**, create or select a platform API Key and store it as the secret `apiKey`. A blank key prevents requests. The Query input `uid` comes from the PDF preupload API. The provider endpoint is fixed; leave timeout and maxResponseBytes blank for their displayed defaults unless an approved network or response requires adjustment.

The [runnable Flow](examples/basic/) uses the static connection `doc2x`. `npm run check` replays all Go-recorded provider scenarios, including retry and invalid response behavior. Live authorization and task state remain unverified without a user-controlled connection.
