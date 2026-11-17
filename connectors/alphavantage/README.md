# Alpha Vantage connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/alphavantage/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/alphavantage`.

This connector reads stock and currency market data through Alpha Vantage's public REST API. The [demo Flows](go/examples/demo/README.md) call `NewGetGlobalQuoteStep` and `NewGetMarketStatusStep` with a static `alphavantage` connection.

The Python package is [`dex-connector-alphavantage`](python/README.md), imported as
`dex_connectors.alphavantage`.

The TypeScript package is [`@superdurable/connector-alphavantage`](typescript/README.md).
Its [runnable demo](typescript/examples/demo/README.md) uses the same static
`alphavantage` connection and the generated `newGetGlobalQuoteStep` factory.

## Operations and official references

| Operation | Query | Official documentation |
| --- | --- | --- |
| `getGlobalQuote` | Latest quoted equity price | [Global Quote](https://www.alphavantage.co/documentation/#latestprice) |
| `getTimeSeriesDaily` | Compact raw daily OHLCV prices | [Daily Time Series](https://www.alphavantage.co/documentation/#daily) |
| `searchSymbols` | Matching stock, ETF, and fund symbols | [Search Endpoint](https://www.alphavantage.co/documentation/#symbolsearch) |
| `getCurrencyExchangeRate` | Latest fiat or crypto pair exchange rate | [Currency Exchange Rate](https://www.alphavantage.co/documentation/#currency-exchange) |
| `getMarketStatus` | Open or closed global trading venues | [Market Status](https://www.alphavantage.co/documentation/#market-status) |
| `getTopGainersLosers` | US gainers, losers, and active tickers | [Top Gainers and Losers](https://www.alphavantage.co/documentation/#top-gainers-losers) |

`getTimeSeriesDaily` requests `outputsize=compact`, which the provider describes as the latest 100 data points. The output preserves decimal price and volume strings to avoid rounding. An empty quote or series selects `notFound`.

`searchSymbols` accepts a nonblank company name or ticker phrase of at most 100 characters. `getCurrencyExchangeRate` requires three-letter uppercase source and destination codes, such as USD and JPY. `getMarketStatus` accepts an optional exact provider region name; blank returns all regions. `getTopGainersLosers` accepts a limit from 1 to 20 per list; zero returns all provider entries. Alpha Vantage provides market data as read-only API calls, so these operations all use the `safe` retry tier.

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
- `searchSymbols`: one IBM match.
- `getCurrencyExchangeRate`: one USD/JPY rate.
- `getMarketStatus`: one US equity venue.
- `getTopGainersLosers`: one ticker in each US market list.

The Go and TypeScript demos use mock-backed real-Dex integration checks. Provider conformance fixtures replay Go-verified requests, branch outcomes, and retry classifications in TypeScript.

## Unverified live behavior

A real Alpha Vantage account key, quote freshness, daily-series entitlement, provider quota response variations, and Dex Web connection form have not been verified. The handling of `Note`, `Information`, and `Error Message` JSON bodies is based on observed provider conventions and needs live confirmation.
