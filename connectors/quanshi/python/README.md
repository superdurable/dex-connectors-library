# Quanshi Python connector

`dex-connector-quanshi` reads accessible Quanshi meeting-room metadata through a Dex Query.

The [runnable Flow](examples/list_rooms/README.md) starts a Worker and Client against Dex, uses a static `ConnectionName`, and verifies the `listed` branch with a synthetic provider. Configure credentials through Dex Connections before using the real provider.

The package replays the [Go-recorded scenarios](../../../conformance/connectors/quanshi/scenarios.json) for provider request and decision parity. Real Quanshi authorization has not been verified.
