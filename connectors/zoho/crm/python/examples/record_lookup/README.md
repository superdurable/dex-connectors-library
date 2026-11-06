# Zoho CRM record lookup

Create a Dex Web Zoho CRM connection named `zoho-crm` using the
[connector authorization guide](../../../connector.yaml). Register the Redirect URI
shown by Dex Web in Zoho API Console, then accept the listed CRM scopes.

Run `dexcli dev -dex-port 40980 -web-port 40981`, set
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40980`, and start the Worker with
`uv run python -m examples.record_lookup.worker` from `python/`.
Start `ZohoCRMRecordLookup` with `GetRecordInput(module="Deals", record_id="<numeric record ID>")`.
The Flow completes with the record; missing records fail on the unwired optional `notFound` branch.

The integration test uses a fake Zoho HTTP endpoint with a real Dex Worker and Client.
It verifies retry after HTTP 503 and the terminal `notFound` route without requiring live Zoho credentials.
