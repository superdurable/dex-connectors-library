# WAYZ current weather

This Go connector reads one live weather observation for a six-digit Chinese city code through the [WAYZ weather API](https://lothub.newayz.com/pdf/api-weather.pdf). `GetCurrentWeatherInput.CityCode` accepts an administrative code such as `420100` for Wuhan. The output contains province, city, provider city code, weather, Celsius temperature, wind, relative humidity, and report time.

Register and apply for an application at [LoTBoard](https://lotboard.newayz.com/). After approval, enable the Weather API and copy the application's AccessKey into the Dex Web `newayz` connection. The key is sent only to the documented HTTPS endpoint and is excluded from Flow outputs and errors. Queries may consume provider quota. Retryable transport and server failures use a bounded retry budget.

See the [runnable Dex Flow](go/examples/current-weather/README.md) for local mock verification. A real WAYZ account and live API call have not been exercised.
