# APISpace Python connector

`dex-connector-apispace` implements the `getCurrentWeather` Query. Configure the
secret token in Dex Web Connections and pass a provider area code from the
[APISpace weather documentation](https://www.apispace.com/eolink/api/456456/apiDocument).
The Query returns city, condition, temperature, humidity, and provider update time.

See the [runnable current-weather Flow](examples/current_weather/) for Worker and
connection setup. Go-recorded conformance scenarios cover request and decision
parity. The checked-in Dex integration test uses a fake provider; real account
authorization and weather data have not been verified.
