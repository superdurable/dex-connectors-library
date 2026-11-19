# Haimou IP location Flow

This runnable Python Flow queries one public IPv4 address through the static `haimou` connection and persists only the coarse location result. Configure the apikey in Dex Web Connections using the connector authorization guide, then run `python -m examples.ip_location.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and a real Dex Worker/Client. It verifies a 503 retry and persisted completion. Run strict FDG 2.0 generation with the command in the package README. Real provider authorization remains unverified.
