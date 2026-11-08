# Buffer connector

The Go, Python, and TypeScript `getChannel` Query reads a connected channel's ID, name, and social service using Buffer's [GraphQL channel query](https://developers.buffer.com/reference). A null channel selects optional `notFound`; GraphQL errors select `providerRejected`. HTTP 429, server failures, and transport failures retry. This operation is read only.

## Connect

In [Buffer Settings → API](https://publish.buffer.com/settings/api), create an account API key and copy its full secret into `apiKey` in Dex Web Connections. The key can access all organizations and channels in the account. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Find a channel's opaque ID by querying [your account's organizations and channels](https://developers.buffer.com/guides/data-model), then pass it as `channelId`. Blank is invalid.

Runnable examples are in [`go/examples/channel-read`](go/examples/channel-read), [`python/examples/channel_read`](python/examples/channel_read), and [`typescript/examples/channel-read`](typescript/examples/channel-read). Go records provider scenarios; Python and TypeScript replay them. Real Buffer authorization and live provider behavior remain unverified without a user-controlled API key.
