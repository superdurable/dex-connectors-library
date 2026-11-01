# Alpha Vantage connector for Python

Install with `pip install dex-connector-alphavantage==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `getGlobalQuote` and `getTimeSeriesDaily` with
the same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getGlobalQuote` | `new_get_global_quote_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getTimeSeriesDaily` | `new_get_time_series_daily_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Both take a ticker symbol of 1 to 25 letters, digits, `.`, `_`, or `-`.
Alpha Vantage reports problems in HTTP 200 bodies: a `Note`, or an
`Information` message about rate limits, call frequency, or requests per day,
retries as a rate limit; another `Information` or an `Error Message` selects
`providerRejected`; an empty quote or series selects `notFound`. HTTP 404 also
selects `notFound`; 408, 429, 5xx, and transport failures retry. The daily
series is returned newest day first.

## Authorization

Create a Dex Web connection named `alphavantage` as described in the
[connector README](../README.md#authorization). Alpha Vantage accepts the API
key only as the `apikey` query parameter, so the key is part of each request
URL; it is resolved for every attempt.

## Example

[`examples/demo`](examples/demo/flow.py) reads a quote and records it. Run the
Worker with `uv run python -m examples.demo.worker` after `dexcli dev` has
loaded a project containing the `alphavantage` connection.

## Tests

`alphavantagemock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
