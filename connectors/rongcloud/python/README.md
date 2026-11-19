# RongCloud Python connector

This package provides the `checkUserOnline` Query for RongCloud domestic IM. Configure the static `rongcloud` connection in Dex Web; the App Secret remains in its secret credential. See [the runnable Worker and Flow](examples/check_user_online/README.md) and [Go-recorded scenarios](../../../conformance/connectors/rongcloud/scenarios.json). Real RongCloud authorization has not been verified.

Run `uv run pytest -q` for conformance and `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/check_user_online/integration_test.py` against a running Dex stack.
