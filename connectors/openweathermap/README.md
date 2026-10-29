# OpenWeatherMap connector

Read current weather and a five-day forecast by coordinates. The manifest is the source of generated Config, Credentials, Connection, operation factories, and mocks. See the [runnable demo](examples/demo/README.md) for Worker and strict FDG 2.0 generation commands.

## Operations and provider references

| Operation | Dex kind | Official API reference |
| --- | --- | --- |
| `getCurrentWeather` | Query | https://openweathermap.org/api/current |
| `getForecast` | Query | https://openweathermap.org/api/forecast5 |

The demo uses `openweathermap.NewGetCurrentWeatherStep` with the static `openweathermap` connection. Each operation is an operation-specific Query Step with the default one-hour retry policy.

## Authorization and configuration

At [OpenWeather account > API keys > Generate](https://home.openweathermap.org/api_keys), create or select an API key for these operations, copy its provider-issued value, and store it in the secret `api_key` field in Dex Web Connections. A blank key is invalid. The manifest supplies the public base URL, HTTP timeout, and response-size limit; leave their fields blank unless a compatible gateway, slow request, or known larger response requires an override.

## Failure behavior

| Condition | Outcome |
| --- | --- |
| 200 with valid bounded output | Happy-path branch |
| 400, 401, 403, or another definite provider rejection | Optional `providerRejected` branch |
| 404 for OpenWeatherMap | Optional `notFound` branch |
| 429, 408, transport error, or 5xx | Retry with the same input until the retry budget ends |
| Malformed or oversized successful response | Optional `invalidResponse` branch |
| Invalid local input or credentials | Optional `defect` branch |

Provider error bodies and secrets are never copied into failures, receipts, or output. Optional branches are unwired in the demo and fail the Flow when selected.

## Mocks

The generated `openweathermapmock` package uses one consistent default answer per operation:

- `getCurrentWeather` default `foundDefault`: `{"city":"Meridian","temperature":21.5,"description":"clear sky"}`
- `getForecast` default `foundDefault`: `{"city":"Meridian","entries":[{"timestamp":1793210400,"temperature":20.1}]}`

`examples/demo/flow/workflow_mock_integration_test.go` starts a real Dex Flow through the mock connection in CI. Provider unit tests use local HTTP servers for the documented request shape and a rate-limit response.

## Unverified live behavior

A real account key, plan access, live current and forecast data, and Dex Web connection form have not been verified. OpenWeatherMap requires `appid` in the URL query; treat HTTP infrastructure logs as secret-bearing and avoid request-URL logging.
