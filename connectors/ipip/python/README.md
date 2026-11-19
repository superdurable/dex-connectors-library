# IPIP Python connector

This package adds `lookupIPLocation` for IPIP's v2 IP location API. Configure the `ipip` connection with the token described in the [company README](../README.md). The operation-specific `new_lookup_ip_location_step` reads one IPv4 or IPv6 address and returns country, region, city, and country code. It exposes `found`, `providerRejected`, `invalidResponse`, and `defect` branches; only `found` is wired by the runnable example.

Run `uv sync --locked` and `uv run pytest` in this directory. The [IP location example](examples/ip_location/) includes a Worker entrypoint and real Dex integration test. It uses `dexcli dev -open=false` and a mock HTTP provider; live IPIP authorization has not been verified.
