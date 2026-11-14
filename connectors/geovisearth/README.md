# GEOVIS Earth connector

[GEOVIS Earth](https://open.geovisearth.com/collect/weather?docId=0) is a Chinese weather platform jointly operated by 中科星图股份有限公司 and 中科星图数字地球合肥有限公司. Its [current weather API](https://open.geovisearth.com/support/document?docId=220&sid=44&source=%E5%A4%A9%E6%B0%94API) documents `GET https://api.open.geovisearth.com/v2/cn/area/basic` with service token and area-code or longitude,latitude input. This connector returns one basic observation: weather description/code, Celsius temperature, humidity, wind, surface pressure, observation time, and area code if supplied. Each request consumes platform call quota; retries are limited to three attempts.

## Connection

Register and verify an account on the [GEOVIS Earth open platform](https://open.geovisearth.com/). Follow the weather API developer guide and apply for a token with access to **全国天气实况（6要素）**. Put that token in the secret `token` field. The provider requires it in the URL query string; the connector never logs or returns it. Avoid HTTP debugging that logs full request URLs. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The connector permits only the documented HTTPS API host.

`GetCurrentWeatherInput.Location` accepts an area code such as `WTX_CH101300201`, or decimal `longitude,latitude`. For coordinates, `areaCode` may be blank in the response. The optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport errors retry within the three-attempt budget; nonzero provider status selects `providerRejected`.

## Example

The [current-weather Flow](go/examples/current-weather/) is a runnable Worker with static Dex connection name `geovisearth`. Its mock is synthetic and is verified with a real Dex Worker and Client. Live authorization, location mapping, quota handling, and current observations remain unverified without a platform token.
