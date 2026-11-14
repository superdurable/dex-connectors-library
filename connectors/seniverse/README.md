# Seniverse current weather

The Go connector reads [current weather](https://docs.seniverse.com/api/weather/now.html) for one location. It returns the location ID/name/country, weather text/code, numeric temperature string and unit, and the provider's last update time. These weather fields are available to free and paid products; premium-only measures are intentionally omitted.

`GetCurrentWeatherInput.Location` accepts a Seniverse city ID, Chinese city name, pinyin name, or `latitude:longitude` pair documented in the [common parameters](https://docs.seniverse.com/api/start/common.html). The connector uses the configured language and temperature unit. The secret private key is sent as Seniverse's documented `key` query parameter over HTTPS; errors and Flow output omit it. Each request consumes one API access from the product quota.

See the [runnable Dex Flow](go/examples/current-weather/README.md) for connection setup and local verification. Real provider authorization and response have not been exercised by mock tests.

The company logo comes from Seniverse's [official brand assets](https://www.seniverse.com/brand).
