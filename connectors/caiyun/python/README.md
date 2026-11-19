# Caiyun Python connector

`dex-connector-caiyun` exposes `getCurrentWeather` as a Dex Query Step. The
[official v2.6 realtime API](https://docs.caiyunapp.com/weather-api/v2/v2.6/1-realtime.html)
reads one coordinate. Mainland China uses GCJ-02 coordinates; elsewhere use
WGS84. The result contains Celsius temperature, humidity as a fraction,
sky condition, and wind speed in meters per second.

At [Caiyun Platform](https://platform.caiyunapp.com), open **访问控制 → API
凭证管理** and create or select an App Key and App Secret pair with v2.6 realtime
access. Both are secret Dex connection fields. The
[signing contract](https://docs.caiyunapp.com/weather-api/v2/v2.6/auth.html)
places the App Key in the provider path and signs each request locally with the
App Secret. Leave endpoint, timeout, and response size blank for their manifest
defaults. Each request consumes a plan call; the manifest limits retries to two.

The [runnable Python Flow](examples/current_weather/) persists one bounded
metric observation using the static `caiyun` connection. Go-recorded scenarios
cover signing and response decisions. Real platform credentials and weather
responses have not been verified locally.
