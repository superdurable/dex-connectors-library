# Baidu Maps connector

The Go connector geocodes one Chinese structured address through the
[Baidu Maps Geocoding API v3](https://lbsyun.baidu.com/docs/webapi?title=geocoding/guide/webservice-geocoding-base).
It exposes a typed `GeocodeAddress` Query and a runnable
[`address-lookup` Flow](go/examples/address-lookup/README.md).

After release, install
`github.com/superdurable/dex-connectors-library/connectors/baidu/maps/go@v0.1.0`.
Create a server-side AK in [Baidu Maps Application Management](https://lbs.baidu.com/apiconsole/key)
with geocoding enabled and an IP whitelist for Dex Worker egress IPs. The
connector supports IP whitelist validation, not SN-signature validation.
The AK travels only in Baidu's required `ak` query parameter; request URLs and
transport errors are never copied into results or logs.

The output uses Baidu's default BD-09 latitude and longitude and includes its
`precise`, `confidence`, `comprehension`, and `level` values. HTTP 408, 429, and
5xx and Baidu status 1 retry; other nonzero provider statuses select
`providerRejected`. Invalid input selects `defect`, and malformed or oversized
responses select `invalidResponse`.

## Verification

```bash
cd connectors/baidu/maps/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Provider tests use a local HTTP server. The example integration test uses a
real Dex Worker and Client with a generated mock. A live Baidu Maps lookup
requires a user-configured server-side AK and is not part of these tests.

## Python

The [Python package](python/README.md) implements `geocodeAddress` with a runnable Dex Flow and Go-recorded provider scenarios. Live Baidu Maps authorization with a real server AK remains unverified.

## TypeScript

The first TypeScript release is `@superdurable/connector-baidu-maps` v0.1.0. See the [TypeScript README](typescript/README.md) and its runnable [address lookup Flow](typescript/examples/address-lookup/README.md). It replays the Go-recorded provider scenarios and has a local real Dex Worker/Client test. Live Baidu AK authorization has not been verified.
