# Read QWeather current weather

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `qweather` connection in Dex Web using your account-specific API Host and API KEY. Start `QWeatherCurrentWeather` with `{"latitude":39.92,"longitude":116.41}`. The default mock returns Beijing weather without calling QWeather. Display the returned attributions alongside weather data.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/current-weather/flow -run TestGetCurrentWeatherUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
