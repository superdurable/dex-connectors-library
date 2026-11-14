# Amap connector

The Go connector geocodes a structured Chinese address through Amap's
[Web Service geocoding API](https://lbs.amap.com/api/webservice/guide/api/georegeo).
It exposes the `GeocodeAddress` Query Step and a runnable
[`address-lookup` Flow](go/examples/address-lookup/README.md).

Install `github.com/superdurable/dex-connectors-library/connectors/amap/go@v0.1.0`
after the first release. Use a Web Service Key from
[Amap Application Management](https://console.amap.com/dev/key/app).

The connection name in the example is `amap`. The only user-supplied connection
value is the secret `api_key`; it is passed to Amap as the required `key` query
parameter. Request URLs and transport errors are never copied into results or
logs. Amap is the source of coordinates; an empty successful result is not a
failure. Returned coordinates are Amap's location values and applications
must apply the provider's map-coordinate usage rules.

The Query retries HTTP 408, 429, and 5xx responses and transport failures under
Dex's default one-hour retry budget. Amap `infocode` 10004 and 10014 also retry;
other `status: 0` responses select `providerRejected`. Missing credentials or
invalid input select `defect`; malformed or oversized responses select
`invalidResponse`.

## Verification

```bash
cd connectors/amap/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

The provider tests use an HTTP server and do not require a live Key. The
example's Dex integration test requires `dexcli dev`. A live Amap call requires
a user-configured Web Service Key in Dex Web Connections; it is not run by the
provider tests.
