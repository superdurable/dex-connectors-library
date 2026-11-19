# 51Tracking status-count Flow

This runnable Python Flow queries aggregate parcel statuses through the static `fiftyonetracking` connection. Configure its API key in Dex Web Connections following the connector's authorization guide, then run `python -m examples.status_counts.worker` from the Python package directory with `DEX_FLOW_SERVICE_ADDRESS` set to the Dex Flow service.

The integration test uses a fake HTTP provider and a real Dex Worker/Client. It verifies a 503 retry and persisted completion without real customer tracking data. Run strict FDG 2.0 generation with the command in the package README. Real provider authorization remains unverified.
