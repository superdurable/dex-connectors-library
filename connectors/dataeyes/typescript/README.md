# DataEyes Web Reader for Dex TypeScript

`@superdurable/connector-dataeyes` reads one public HTTPS page through DataEyes Web Reader and returns its title and bounded Markdown. The `readWebPage` Query accepts `{ url }` and selects `found`, or optional `providerRejected`, `invalidResponse`, or `defect`. Transient provider failures return Retry; the manifest permits one retry because a successful read can consume quota.

## Connection

Create a Dex Web Connection named `dataeyes`. In the [DataEyes console key guide](https://doc.shuyanai.com/doc-8441886), open **MCP产品 → 接入与密钥** and copy the secret search/reading Key into `apiKey`. A model API Key does not work. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults. The fixed endpoint is `https://api.shuyanai.com/v1/reader`; `format` is fixed to `markdown`.

The input URL must be a public HTTPS page with a domain name, no user info, query, or fragment. The result contains only `title` and up to 100000 bytes of `markdown`; secrets, raw provider metadata, and error text are excluded.

## Runnable Flow

The [web reader example](examples/web-reader/README.md) is a complete Dex Worker and Client application. It persists the result and uses the static `dataeyes` connection. Install with `npm install`, run `npm run example:worker`, and start with `npm run example:start -- https://example.com` after configuring Dex.

Provider conformance replays Go recordings in `test/conformance.test.ts`. The integration test runs the same Flow against a real Dex Worker with a local provider fixture. Real DataEyes authorization and quota use remain unverified without a user-owned connection.
