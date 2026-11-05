# Zoho CRM record lookup

This runnable Flow reads one Zoho CRM record through the static `zoho-crm-main` connection and completes with its module and ID. Configure that connection in Dex Web, then run `python -m examples.record_lookup.main` from `connectors/zoho/crm/python` with `DEX_SERVER_ADDRESS` pointing at your Dex server.

To verify with a real local Dex stack, start `dexcli dev -dex-port 40840 -web-port 40841 -open=false`, set `DEX_SERVER_ADDRESS=127.0.0.1:40840`, and run `uv run pytest examples/record_lookup/tests/test_integration.py`. The integration tests use inert credentials and a local HTTP fake; real Zoho authorization and API access remain unverified.
