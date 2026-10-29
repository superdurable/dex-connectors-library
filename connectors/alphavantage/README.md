# Alpha Vantage connector

This connector reads stock market data through Alpha Vantage's public REST API. The [demo Flow](examples/demo/README.md) calls `NewGetGlobalQuoteStep` with a static `alphavantage` connection.

## Operations and official references

| Operation | Query | Official documentation |
| --- | --- | --- |
| `getGlobalQuote` | Latest quoted equity price | [Global Quote](https://www.alphavantage.co/documentation/#latestprice) |
| `getTimeSeriesDaily` | Compact raw daily OHLCV prices | [Daily Time Series](https://www.alphavantage.co/documentation/#daily) |

`getTimeSeriesDaily` requests `outputsize=compact`, which the provider describes as the latest 100 data points. The output preserves decimal price and volume strings to avoid rounding. An empty quote or series selects `notFound`.

## Authorization

Claim a key at [Alpha Vantage API key support](https://www.alphavantage.co/support/api-key/). Enter the provider-issued string in Dex Web Connections' secret `api_key` field; blank is invalid. The API requires `apikey` in the URL query, so HTTP infrastructure request-URL logs must be treated as secret-bearing. Connector failures, receipts, mock results, and Flow output never include the key. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for manifest defaults.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| Valid HTTP 200 data | `found` |
| Empty quote or series | Optional `notFound` |
| Provider rejection, including an `Error Message` or non-rate-limit `Information` body | Optional `providerRejected` |
| HTTP 429, 408, 5xx, transport failure, `Note` or rate-limit `Information` | Retry until the default one-hour budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` |
| Invalid local input or credentials | Optional `defect` |

Provider response text is not copied into failures. The demo wires only `found`; optional branches fail the Flow when selected.

## Mocks

The generated `alphavantagemock` package supplies these consistent defaults:

- `getGlobalQuote`: IBM at 255.25 on 2026-10-28.
- `getTimeSeriesDaily`: IBM's 2026-10-28 raw OHLCV point, closing at 255.25.

The demo's mock-backed real-Dex integration test runs in CI. Provider unit tests use a local HTTP server for the API query and body classification.

## Unverified live behavior

A real Alpha Vantage account key, quote freshness, daily-series entitlement, provider quota response variations, and Dex Web connection form have not been verified. The handling of `Note`, `Information`, and `Error Message` JSON bodies is based on observed provider conventions and needs live confirmation.
