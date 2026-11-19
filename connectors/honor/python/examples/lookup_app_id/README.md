# Honor app ID lookup Flow

This runnable Python Flow looks up one owned Android package through the static `honor` connection and persists only its package name and app ID. Configure the Client ID and API key in Dex Web Connections using the connector authorization guide, then run `python -m examples.lookup_app_id.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and a real Dex Worker/Client. It verifies a 503 retry after token exchange and persisted completion. Run strict FDG 2.0 generation with the command in the package README. Real provider authorization remains unverified.
