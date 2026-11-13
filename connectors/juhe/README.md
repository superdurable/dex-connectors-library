# Juhe Data current weather

The Go connector reads current conditions for one Chinese city from [Juhe Data API 73](https://www.juhe.cn/docs/api/id/73). The [official API PDF](https://www.juhe.cn/docs/download/pdf/73) documents the HTTPS `simpleWeather/query` endpoint, `city` and `key` parameters, response schema, and error codes. The connector returns only `result.city` and `result.realtime` fields; it discards the forecast array. Temperature, humidity, wind, and AQI may be empty according to the provider contract.

The API key is sent in the required HTTPS query parameter and remains a Dex credential. The [current API catalog](https://www.juhe.cn/docs/o3/19) lists 50 free calls per day, with higher membership limits. Transport failures, HTTP 429/5xx, and documented temporary service errors retry at most once to limit quota use. Authentication, authorization, invalid city, and exhausted quota are terminal branches.

See the [runnable Dex Flow](go/examples/current-weather/README.md). Mock tests do not verify live authorization, quota behavior, or provider responses.
