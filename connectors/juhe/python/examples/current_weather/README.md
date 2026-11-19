# Juhe getCurrentWeather example

Run `uv run python -m examples.current_weather.worker` after configuring the static `juhe` connection in Dex Web. Run `uv run pytest examples/current_weather/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
