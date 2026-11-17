# APISpace current weather

This Go connector reads current observations for one city through the [APISpace Weather Forecast Query API](https://www.apispace.com/eolink/api/456456/apiDocument). `GetCurrentWeather` takes a provider city `areaCode` and returns the city name, current condition, temperature in °C, relative humidity in percent, and provider update time in Beijing local time. It does not read customer data.

APISpace is operated in China by 深圳市银云信息技术有限公司, as stated on its [official site](https://blog.apispace.com/apispace-%E5%A4%A9%E6%B0%94%E9%A2%84%E6%8A%A5%E6%9F%A5%E8%AF%A2api%E6%8E%A5%E5%8F%A3%E6%A1%88%E4%BE%8B%E4%BB%A3%E7%A0%81/). The endpoint is `GET https://eolink.o.apispace.com/456456/weather/v001/now?areacode=<city ID>`, authenticated by `X-APISpace-Token` and `Authorization-Type: apikey`. In APISpace, subscribe to the weather product and find the API key in Console > Access Control. The [official request and response example](https://blog.apispace.com/apispace-%E5%A4%A9%E6%B0%94%E9%A2%84%E6%8A%A5%E6%9F%A5%E8%AF%A2api%E6%8E%A5%E5%8F%A3%E6%A1%88%E4%BE%8B%E4%BB%A3%E7%A0%81/) identifies the city list attachment and response fields. The [product documentation](https://www.apispace.com/eolink/api/456456/apiDocument) explains key handling and plan errors. Calls may consume paid plan allowance. HTTP 413 (frequency limit), 429, 5xx, and transport failures retry; HTTP 416 (unsubscribed or exhausted quota) is a terminal provider rejection.

The [runnable Dex Flow](go/examples/current-weather/README.md) shows setup and typed output. [APISpace's official logo](https://www.apispace.com/logos/APISpace.svg) is included unchanged. Live authorization and production weather responses remain unverified.

## Python

The [Python package](python/README.md) implements `getCurrentWeather` and includes
a runnable Dex Flow plus Go-recorded provider conformance scenarios.
