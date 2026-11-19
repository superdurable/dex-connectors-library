# WeCom application Flow

`flow.py` sends to members, departments, or tags through the static `wecom-application` connection. Configure CorpID, AgentID, CorpSecret, and trusted Worker IP in Dex Web Connections using the [manifest authorization guide](../../../connector.yaml), then start the Worker with `uv run python -m examples.worker` after `dexcli dev -open=false` is running. The Step enables WeCom's duplicate check and bounds sends with a heartbeat Fence.

The checked-in integration test uses synthetic WeCom token and send responses with a real Dex Worker and Client. Live delivery remains unverified.
