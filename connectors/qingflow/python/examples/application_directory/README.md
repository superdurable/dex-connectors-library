# Qingflow application directory

Run `python -m examples.application_directory.worker` with a Qingflow connection named `qingflow` configured through Dex Web. The Flow uses the super-admin token to list application keys and names, then persists the result. Optional failure branches are intentionally unwired and fail the Flow when selected.

`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest -q examples/application_directory/integration_test.py` verifies one 503 retry against a real `dexcli dev -open=false` server and a local fake provider. Live Qingflow authorization has not been verified.
