# iP138 token usage Flow

This runnable Python Flow reads token usage and quota through the static `ip138` connection and persists only the three count fields. Configure the token in Dex Web Connections using the connector authorization guide, then run `python -m examples.token_usage.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and real Dex Worker/Client. It verifies a 503 retry and persisted completion. Run strict FDG 2.0 generation with the package README command. Live authorization remains unverified.
