# Upstash Redis connector

This connector sends Redis commands through the [Upstash REST API](https://upstash.com/docs/redis/features/restapi). It requires an Upstash database with its REST endpoint enabled; it does not connect to a generic Redis TCP server.

| Operation | Command | Repeat tier |
| --- | --- | --- |
| `getKey` | `GET` | safe |
| `setKey` | `SET`, with optional absolute `EXAT` expiration | safe |
| `deleteKey` | `DEL` | safe |
| `listKeys` | `KEYS` glob lookup | safe |
| `getListLength` | `LLEN` | safe |
| `getInfo` | `INFO` | safe |
| `pushListItem` | `LPUSH` or `RPUSH` | limited, at most three possible sends by default |
| `publishMessage` | `PUBLISH` | limited, at most three possible sends by default |

`getKey` returns `KeyLookup {key, value, exists}`. Its `found` branch handles an existing string, including an empty one; `missing` handles an absent key. `setKey` replaces the complete value. Its optional `expireAtUnixSeconds` is an absolute Unix timestamp, so a retry does not extend the key's lifetime. `listKeys` accepts a Redis glob pattern; blank means `*`, which can be expensive on a large database. `getListLength` returns zero for an absent list. `pushListItem` accepts `left` or `right` (blank means right). Both limited operations return `uncertain` when their possible-send cap is reached without confirmation. Rate limits, HTTP 503, and server errors retry as specified by each operation's tier. Provider rejections use optional branches.

In [Upstash Console](https://console.upstash.com), open **Redis > selected database > Connect > REST**. Copy the HTTPS `UPSTASH_REDIS_REST_URL` into the non-secret `baseUrl` field. Copy the standard `UPSTASH_REDIS_REST_TOKEN` into secret `rest_token` for writes; the read-only token supports queries only. Keep `timeout` and `maxResponseBytes` blank for their displayed defaults. In **Data Browser**, find case-sensitive key and list names. Reset the database password to revoke the token. Live Upstash authorization and provider calls have not been verified.

Runnable examples: [Go](go/examples/key-lookup/README.md), [Python](python/examples/key_lookup/README.md), [TypeScript](typescript/examples/key-lookup/README.md).
