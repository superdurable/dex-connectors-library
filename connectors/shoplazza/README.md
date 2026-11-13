# Shoplazza connector

This Go connector exposes the read-only `GetShop` Query and a runnable [shop inspection Flow](go/examples/shop-inspection/README.md). It calls Shoplazza's [current v202701 Get shop API](https://www.shoplazza.dev/api/202701/shop-detail) with the `read_shop` permission. The request selects only `id`, `name`, `domain`, and `currency`; contact and address fields are not retained.

After release, install `github.com/superdurable/dex-connectors-library/connectors/shoplazza/go@v0.1.0`. Configure the store's `*.myshoplaza.com` system domain and a protected private-app Access Token in Dex Web Connections. Shoplazza's [private-app guide](https://www.shoplazza.dev/docs/app/getting-started/create-private-app) explains Apps > Manage Private Apps > Create App, granting `read_shop`, and viewing the generated token. The [API usage guide](https://www.shoplazza.dev/docs/app/building-blocks/rest-api/api-usage) defines the HTTPS host and `Access-Token` header. The [changelog](https://www.shoplazza.dev/changelog) marks v202701 current, released 2026-10-10.

The Query retries transport errors, HTTP 408, 429 (respecting `Retry-After`), and 5xx responses under Dex's default one-hour budget. A definitive provider rejection selects `providerRejected`; invalid credentials select `defect`; malformed or oversized responses select `invalidResponse`. Provider response bodies and credentials are not copied into failures, receipts, or Flow results.

## Verification

```bash
cd connectors/shoplazza/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Provider tests use a local HTTP server. The example integration test uses a real Dex dev server and generated provider mock. Live Shoplazza authorization, `read_shop` grant, token rotation, and API behavior need verification with a user-controlled store connection.
