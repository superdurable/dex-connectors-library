# Caiyun Weather current weather

This connector reads [Caiyun Weather v2.6 realtime observations](https://docs.caiyunapp.com/weather-api/v2/v2.6/1-realtime.html) for one longitude and latitude. Mainland China coordinates must use GCJ-02; other locations, including Hong Kong and Taiwan, use WGS84. Output contains Celsius temperature, relative humidity (0–1), a sky condition code, and wind speed (m/s).

The connector uses [App Key and App Secret signing](https://docs.caiyunapp.com/weather-api/v2/v2.6/auth.html) through HTTPS. Both are secret Dex connection fields. The App Key appears in the provider URL path as required; the App Secret remains local. Neither is returned in Flow results. Requests consume [plan calls](https://docs.caiyunapp.com/weather-api/billing.html); retries are limited to two attempts because an ambiguous failure may consume an extra call. Every HTTP 429, including QPS and quota limits, retries and honors `Retry-After`; when retries run out the Flow fails.

See the [runnable Flow](go/examples/current-weather/README.md). The mock uses synthetic Beijing weather and no provider credentials. Live signed authorization and weather data have not been verified.

The [Python package](python/) includes a runnable current-weather Flow and Go-recorded conformance replay.
