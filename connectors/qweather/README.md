# QWeather connector

This connector implements the read-only [current weather API](https://dev.qweather.com/en/docs/api/weather/weather-current/) for decimal latitude and longitude. It calls the account-specific HTTPS API Host with `X-QW-Api-Key` authentication and returns current condition, temperature, observation time, and provider attributions. The host is part of QWeather authentication and is stored with the key as a Dex credential. Neither appears in Flow results, receipts, Attributes, or logs.

Create a project and API KEY credential in the [QWeather Console](https://console.qweather.com/), then copy your account's API Host from Settings. QWeather requires its dedicated API Host; legacy shared hosts are being retired. Invalid coordinates fail locally, while provider errors select `providerRejected` and transient failures retry up to three times. QWeather states that API KEY authentication will have a daily call-volume limit from 2027-02-01; this connector currently uses the documented API KEY method.

The runnable [current-weather example](go/examples/current-weather/README.md) demonstrates the generated operation-specific Step and static `qweather` connection. Real QWeather authorization remains to be tested with a user-controlled Dex Web Connection.
