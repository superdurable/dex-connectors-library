# Upstash Redis connector

`getKey` reads one Redis string key through the [Upstash REST API](https://upstash.com/docs/redis/features/restapi). It sends a JSON `GET` command and returns `KeyLookup {key, value, exists}`. The `found` branch handles existing string values, including empty strings; the optional `missing` branch handles absent keys. Rate limits, transport errors, HTTP 408, and server errors retry. Other provider rejections and invalid responses use optional branches. This connection requires an Upstash Redis database with its REST endpoint enabled; it does not connect to a generic Redis TCP server.

In [Upstash Console](https://console.upstash.com), open **Redis > selected database > Connect > REST**. Copy the HTTPS `UPSTASH_REDIS_REST_URL` into the non-secret `baseUrl` field. Enable **Read-Only Token** and copy `UPSTASH_REDIS_REST_TOKEN` into secret `rest_token`. Keep `timeout` and `maxResponseBytes` blank for their displayed defaults. In **Data Browser**, find the case-sensitive string key to read. Reset the database password to revoke the token. Live Upstash authorization and provider calls have not been verified.

Runnable examples: [Go](go/examples/key-lookup/README.md), [Python](python/examples/key_lookup/README.md), [TypeScript](typescript/examples/key-lookup/README.md).
