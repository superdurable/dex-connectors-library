# OpenWeatherMap connector for Python

Install with `pip install dex-connector-openweathermap==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `getCurrentWeather` and `getForecast` with the
same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getCurrentWeather` | `new_get_current_weather_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getForecast` | `new_get_forecast_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Both take a latitude from -90 to 90 and a longitude from -180 to 180 in decimal
degrees, plus `units` of `standard` (the default, kelvin), `metric`, or
`imperial`. HTTP 404 selects `notFound`; 408, 429, 5xx, and transport failures
retry; other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `openweathermap` as described in the
[connector README](../README.md#authorization-and-configuration).
OpenWeatherMap accepts the API key only as the `appid` query parameter, so the
key is part of each request URL; it is resolved for every attempt.

## Example

[`examples/demo`](examples/demo/flow.py) reads the current weather for a
coordinate and records it. Run the Worker with
`uv run python -m examples.demo.worker` after `dexcli dev` has loaded a project
containing the `openweathermap` connection.

## Tests

`openweathermapmock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
