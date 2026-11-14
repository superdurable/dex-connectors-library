# RongCloud connector

The Go connector checks one IM user's current online status through RongCloud's
[Server API](https://docs.rongcloud.cn/platform-chat-api/user/check-online-status-by-user).
It exposes the `CheckUserOnline` Query Step and a runnable
[`user-online-status` Flow](go/examples/user-online-status/README.md).

Install `github.com/superdurable/dex-connectors-library/connectors/rongcloud/go@v0.1.0`
after the first release. Create a domestic-data-center IM application and copy
its App Key and App Secret from the [RongCloud console](https://console.rongcloud.cn/agile/apps/km).
The App Secret remains in Dex Connections and is used locally to sign each
request. The request carries the App Key, fresh nonce (18 hexadecimal
characters), Unix-millisecond timestamp, and SHA1 hex signature of
`App Secret + Nonce + Timestamp`. The `userId` is sent in an
`application/x-www-form-urlencoded` POST body. Results contain only `isOnline`;
provider response bodies, user IDs, and credentials are never copied into
results or logs.

The Query retries HTTP 408, 429, 5xx, transport errors, and documented provider
rate-limit codes 1008/20607 and timeout code 1050 under Dex's default one-hour
budget. HTTP auth/permission errors and other provider rejections select
`providerRejected`; malformed or oversized replies select `invalidResponse`.
Invalid user IDs or credentials select `defect`. The documented online status
`"1"` maps to true and `"0"` maps to false. The provider limits this endpoint
to 100 calls per second by default; callers should avoid rapid polling.

## Verification

```bash
cd connectors/rongcloud/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Provider tests use a local HTTP server. The example's integration test uses a
real Dex Worker and Client with a generated mock. A real application and
Dex Web connection are needed for live verification.

References: [signing](https://docs.rongcloud.cn/platform-chat-api/auth),
[domestic API domains](https://docs.rongcloud.cn/platform-chat-api/base-url), and
[status codes](https://docs.rongcloud.cn/platform-chat-api/code).
