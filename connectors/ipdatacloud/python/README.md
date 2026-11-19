# IPDataCloud Python connector

This package adds `lookupIPLocation` for IPDataCloud's IP location API. Configure the `ipdatacloud` connection with the API key described in the [company README](../README.md). The operation-specific `new_lookup_ip_location_step` reads one public IPv4 address and returns country, province, city, district, and operator. It exposes `found`, `providerRejected`, `invalidResponse`, and `defect` branches; only `found` is wired by the runnable example.

Run `uv sync --locked` and `uv run pytest` in this directory. The [IP location example](examples/ip_location/) includes a Worker entrypoint and real Dex integration test. It uses `dexcli dev -open=false` and a mock HTTP provider; live IPDataCloud authorization has not been verified.
