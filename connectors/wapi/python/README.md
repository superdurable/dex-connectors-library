# Wapi Python connector

`dex-connector-wapi` reads vehicle brands and maintenance-record query prices in Chinese fen through a Dex Query.

The [runnable Flow](examples/list_vehicle_brands/README.md) starts a Worker and Client against Dex, uses a static `ConnectionName`, and verifies the `found` branch with a synthetic provider. Configure the application ID and secret through Dex Connections before using the real provider.

The package replays the [Go-recorded scenarios](../../../conformance/connectors/wapi/scenarios.json) for provider request and decision parity. Real Wapi authorization has not been verified.
