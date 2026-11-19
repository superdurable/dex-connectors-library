# WeCom group robot Flow

`flow.py` posts one text or markdown message through the static `wecom-robot` connection. Configure its webhook key in Dex Web Connections using the [manifest authorization guide](../../../connector.yaml), then start the Worker with `uv run python -m examples.worker` after `dexcli dev -open=false` is running. The group robot accepts no idempotency key, so this Step uses a heartbeat Fence and a bounded send cap.

The checked-in integration test uses a synthetic WeCom HTTP response with a real Dex Worker and Client. Live delivery remains unverified.
