# Guance dashboard-page Flow

This runnable Python Flow lists one page of dashboard UUIDs and names through the static `guance` connection. Configure a workspace API key in Dex Web Connections using the connector's authorization guide, then run `python -m examples.dashboard_page.worker` from this package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and a real Dex Worker/Client. It verifies a 503 retry and persisted completion. Run strict FDG 2.0 generation with the command in the package README. Real provider authorization remains unverified.
