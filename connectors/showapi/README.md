# ShowAPI

The Go connector calls ShowAPI's [weather region-code lookup](https://www.showapi.com/apis/weather-forecast-query-place-code-9-3). `LookupAreaCodes` accepts a place name and returns only province, area, and weather area code. It does not expose the provider's additional location details or any personal data.

Sign in to the [ShowAPI console](https://www.showapi.com/console#/myApp), find **AppKey Management**, and copy the AppKey into the secret Dex Web connection field. Order the Weather Forecast product before calling endpoint 9-3. The API uses paid resource packages, including a trial option; consult the selected package for call allowance and frequency. The AppKey is sent in an HTTPS query parameter as required by ShowAPI. The connector never returns the key or provider URL in Flow data or error messages.

The lookup is read-only, but repeated calls may consume additional paid package quota. Dex retries transient failures at most three times within one minute. See the [runnable Dex Flow](go/examples/area-code/README.md). Live ShowAPI authorization and responses have not been exercised.

The company logo comes from [ShowAPI's official site](https://www.showapi.com/nuxt3_static/img/logo_white.svg).
